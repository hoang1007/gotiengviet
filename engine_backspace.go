/*
 * Bamboo - A Vietnamese Input method editor
 * Copyright (C) 2018 Luong Thanh Lam <ltlam93@gmail.com>
 *
 * This program is free software: you can redistribute it and/or modify
 * it under the terms of the GNU General Public License as published by
 * the Free Software Foundation, either version 3 of the License, or
 * (at your option) any later version.
 *
 * This program is distributed in the hope that it will be useful,
 * but WITHOUT ANY WARRANTY; without even the implied warranty of
 * MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
 * GNU General Public License for more details.
 *
 * You should have received a copy of the GNU General Public License
 * along with this program.  If not, see <http://www.gnu.org/licenses/>.
 *
 */

package main

import (
	"fmt"
	"ibus-bamboo/config"
	"log"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/BambooEngine/bamboo-core"
	"github.com/godbus/dbus/v5"
)

const BACKSPACE_INTERVAL = 0

func (e *IBusBambooEngine) bsProcessKeyEvent(keyVal uint32, keyCode uint32, state uint32) (bool, *dbus.Error) {
	if isMovementKey(keyVal) {
		e.preeditor.Reset()
		e.resetFakeBackspace()
		e.isSurroundingTextReady = true
		return false, nil
	}
	// GOTV: capture the focus/field generation at entry, before any wait or
	// processing, so the key is tagged with the field it was pressed in (not
	// the field that happens to be focused later).
	var gen = e.currentInputGen()
	var keyRune = rune(keyVal)
	if e.config.IBflags&config.IBmacroEnabled == 0 && len(keyPressChan) == 0 && e.getRawKeyLen() == 0 && !inKeyList(e.preeditor.GetInputMethod().AppendingKeys, keyRune) {
		e.updateLastKeyWithShift(keyVal, state)
		if e.preeditor.CanProcessKey(keyRune) && isValidState(state) {
			e.isFirstTimeSendingBS = true
			if state&IBusLockMask != 0 {
				keyRune = e.toUpper(keyRune)
			}
			e.preeditor.ProcessKey(keyRune, bamboo.VietnameseMode)
			e.bsCommitText([]rune(e.getPreeditString()))
			return true, nil
		}
		return false, nil
	}

	if e.shouldEnqueuKeyStrokes {
		// WARNING: don't use ForwardKeyEvent api in XTestFakeKeyEvent/SurroundingText mode
		if e.checkInputMode(config.XTestFakeKeyEventIM) || e.checkInputMode(config.SurroundingTextIM) {
			if keyVal == IBusBackSpace {
				if e.getFakeBackspace() > 0 {
					e.addFakeBackspace(-1)
					return false, nil
				} else {
					sleep()
					if e.getRawKeyLen() > 0 {
						if e.shouldFallbackToEnglish(true) {
							e.preeditor.RestoreLastWord(false)
						}
						e.preeditor.RemoveLastChar(false)
					}
				}
				return false, nil
			}
			if keyVal == IBusTab {
				sleep()
				if ok, _ := e.getMacroText(); !ok {
					e.preeditor.Reset()
					return false, nil
				}
			}
			isValidKey := isValidState(state) && e.isValidKeyVal(keyVal)
			if !isValidKey {
				sleep()
				// GOTV: same stale/password drop semantics as the queued path —
				// a key whose field changed while it was processed must not fall
				// through to the client (which would type it into the new field).
				ret := e.keyPressHandler(keyVal, keyCode, state, gen)
				if !ret && e.isCurrentInputGen(gen) && !e.isPasswordField() {
					return false, nil
				}
				return true, nil
			}
		}
		// if the main thread is busy processing, the keypress events come all mixed up
		// so we enqueue these keypress events (tagged with the entry focus
		// generation) and process them sequentially on another thread
		keyPressChan <- keyPressEvent{keyVal, keyCode, state, gen}
		return true, nil
	} else {
		// GOTV: same stale/password drop semantics as the queued path.
		ret := e.keyPressHandler(keyVal, keyCode, state, gen)
		if !ret && e.isCurrentInputGen(gen) && !e.isPasswordField() {
			return false, nil
		}
		return true, nil
	}
}

func (e *IBusBambooEngine) keyPressForwardHandler(keyVal, keyCode, state uint32, gen uint64) {
	ret := e.keyPressHandler(keyVal, keyCode, state, gen)
	if !ret {
		// GOTV: the field may have changed while the handler was working; drop
		// instead of forwarding a stale key into the field that now has focus.
		if e.isCurrentInputGen(gen) && !e.isPasswordField() {
			e.ForwardKeyEvent(keyVal, keyCode, state)
		}
	}
}

func (e *IBusBambooEngine) keyPressHandler(keyVal, keyCode, state uint32, gen uint64) bool {
	// GOTV: a focus/field change while the key was queued invalidates it — it
	// belongs to the previous field, never compose/rewrite it into the new one.
	if !e.isCurrentInputGen(gen) {
		return true
	}
	// GOTV: even if this key was queued before the field switched, never compose
	// or commit into a password field once the worker reaches it.
	if e.isPasswordField() {
		return true
	}
	// log.Printf(">>Backspace:ProcessKeyEvent >  %c | keyCode 0x%04x keyVal 0x%04x | %d\n", rune(keyVal), keyCode, keyVal, len(keyPressChan))
	defer e.updateLastKeyWithShift(keyVal, state)
	if e.keyPressDelay > 0 {
		time.Sleep(time.Duration(e.keyPressDelay) * time.Millisecond)
		e.keyPressDelay = 0
	}
	oldText := e.getPreeditString()
	_, oldMacText := e.getMacroText()
	if keyVal == IBusBackSpace {
		if e.getRawKeyLen() > 0 {
			if e.config.IBflags&config.IBautoNonVnRestore == 0 {
				e.preeditor.RemoveLastChar(false)
				return false
			}
			e.preeditor.RemoveLastChar(true)
			var newText = e.getPreeditString()
			var offset = e.getPreeditOffset([]rune(newText), []rune(oldText))
			if oldText != "" && offset != len([]rune(newText)) {
				e.updatePreviousText(oldText, newText, gen)
				return true
			}
		}
		return false
	}

	if keyVal == IBusTab {
		defer e.preeditor.Reset()
		if oldMacText != "" {
			e.updatePreviousText(oldText, oldMacText, gen)
			return true
		}
		return false
	}

	isValidKey := isValidState(state) && e.isValidKeyVal(keyVal)
	newText, isWordBreakRune := e.getCommitText(keyVal, keyCode, state)
	if len(newText) > 0 {
		// GOTV: re-validate before any output — composing the key may take long
		// enough that focus changed or a password field appeared; abort so we
		// never send backspaces into / commit into the field that now has focus.
		if !e.isCurrentInputGen(gen) || e.isPasswordField() {
			return isValidKey
		}
		if e.shouldAppendDeadKey(newText, oldText) {
			fmt.Println("Append a deadkey")
			e.bsCommitText([]rune(" "))
			time.Sleep(10 * time.Millisecond)
			e.isFirstTimeSendingBS = false
			e.SendBackSpace(1, gen)
		}
		// GOTV: batch-commit the whole word at once. updatePreviousTextInBatch
		// applies this key's rewrite and drain-commits every keystroke already
		// queued, delivering the accumulated text in ONE commitText at the next
		// word break (or when the queue drains). Batching avoids the
		// stand-alone multi-run commit that lands right after forwarded
		// backspaces, which GTK/Qt drop at fast typing rates — the source of
		// the reported character loss (thấy→thy, mất→mt).
		e.updatePreviousTextInBatch(oldText, newText, isWordBreakRune, gen)
		return isValidKey
	}
	return isValidKey
}

func (e *IBusBambooEngine) getPreeditOffset(newRunes, oldRunes []rune) int {
	var minLen = len(oldRunes)
	if len(newRunes) < minLen {
		minLen = len(newRunes)
	}
	for i := 0; i < minLen; i++ {
		if oldRunes[i] != newRunes[i] {
			return i
		}
	}
	return minLen
}

func (e *IBusBambooEngine) shouldAppendDeadKey(newText, oldText string) bool {
	var oldRunes = []rune(oldText)
	var newRunes = []rune(newText)
	var offset = e.getPreeditOffset(newRunes, oldRunes)

	// workaround for chrome and firefox's address bar
	if e.isFirstTimeSendingBS && offset < len(newRunes) && offset < len(oldRunes) && e.inBrowserList() &&
		!e.checkInputMode(config.ShiftLeftForwardingIM) {
		return true
	}
	return false
}

// updatePreviousTextInBatch applies one key's committed-text update and
// batch-drains every keystroke already queued, committing the whole word in ONE
// commitText at the next word break (or when the queue drains). Batching
// avoids the stand-alone multi-run commit that would otherwise land right after
// forwarded backspaces, which GTK/Qt drop at fast typing rates — the source of
// the reported character loss (thấy→thy, mất→mt).
func (e *IBusBambooEngine) updatePreviousTextInBatch(oldText, newText string, isWordBreakRune bool, gen uint64) {
	offsetRunes, nBackSpace := e.getOffsetRunes(newText, oldText)
	if nBackSpace > 0 {
		e.SendBackSpace(nBackSpace, gen)
		// GOTV: re-validate after the (long) backspace window — the field may
		// have changed or become a password field mid-rewrite.
		if !e.isCurrentInputGen(gen) || e.isPasswordField() {
			return
		}
	}
	var buffer = []string{string(offsetRunes)}
	if isWordBreakRune {
		e.preeditor.Reset()
		buffer = append(buffer, "")
	}
	// isDirty means containing runes that are not committed
	var isDirty = false
	for i := 0; i < len(keyPressChan); i++ {
		var keyEvents = <-keyPressChan
		// GOTV: drop keys queued under a previous focus generation — never
		// compose them against the field that now has focus.
		if !e.isCurrentInputGen(keyEvents.gen) {
			continue
		}
		var keyVal, keyCode, state = keyEvents.keyVal, keyEvents.keyCode, keyEvents.state
		isValidKey := isValidState(state) && e.isValidKeyVal(keyVal)
		if isValidKey {
			var commitText, isWordBreakRune0 = e.getCommitText(keyVal, keyCode, state)
			buffer[len(buffer)-1] = commitText
			if isWordBreakRune0 {
				buffer = append(buffer, "")
			}
			isDirty = true
		} else {
			if isDirty {
				e.batchCommit(oldText, strings.Join(buffer, ""), nBackSpace, isWordBreakRune, gen)
				buffer = []string{""}
			}
			e.ForwardKeyEvent(keyVal, keyCode, state)
		}
	}
	if isDirty {
		e.batchCommit(oldText, strings.Join(buffer, ""), nBackSpace, isWordBreakRune, gen)
		return
	}
	log.Printf("Updating Previous Text %s ---> %s\n", oldText, newText)
	e.bsCommitText(offsetRunes)
}

// batchCommit compares two given text and commit the right outer text, with
// backspaces if necessary:
// toi - tôi = ôi + 2 BS
// <space> - tôi = tôi
func (e *IBusBambooEngine) batchCommit(oldText string, newText string, nBackSpace int, isWordBreakRune bool, gen uint64) {
	// GOTV: re-validate before emitting anything — abort if the field changed
	// or became a password field while draining/rewriting.
	if !e.isCurrentInputGen(gen) || e.isPasswordField() {
		return
	}
	fullRunes := []rune(newText)
	if len(fullRunes) == 0 {
		return
	}
	patchedRunes, patchedBackSpace := e.getOffsetRunes(newText, oldText)
	if isWordBreakRune {
		e.bsCommitText(patchedRunes)
		return
	}
	if patchedBackSpace > nBackSpace {
		e.SendBackSpace(patchedBackSpace-nBackSpace, gen)
	} else if patchedBackSpace < nBackSpace {
		var offset = utf8.RuneCountInString(oldText) - nBackSpace
		patchedRunes = fullRunes[offset:]
	}
	log.Printf("\nUpdating Previous Text %s ---> %s\n", oldText, newText)
	fmt.Print("====================================\n\n")
	e.bsCommitText(patchedRunes)
}

// updatePreviousText applies a rewrite immediately (no deferral) — used only
// by Backspace/Tab key handling where the user expects the change at once.
func (e *IBusBambooEngine) updatePreviousText(oldText, newText string, gen uint64) {
	offsetRunes, nBackSpace := e.getOffsetRunes(newText, oldText)
	if nBackSpace > 0 {
		if !e.isCurrentInputGen(gen) || e.isPasswordField() {
			return
		}
		e.SendBackSpace(nBackSpace, gen)
	}
	if !e.isCurrentInputGen(gen) || e.isPasswordField() {
		return
	}
	log.Printf("Updating Previous Text %s ---> %s\n", oldText, newText)
	e.bsCommitText(offsetRunes)
}

// getOffsetRunes returns the right outer text and number of pending backspaces
func (e *IBusBambooEngine) getOffsetRunes(newText, oldText string) ([]rune, int) {
	var oldRunes = []rune(oldText)
	var newRunes = []rune(newText)
	var nBackSpace = 0
	var offset = e.getPreeditOffset(newRunes, oldRunes)
	if offset < len(oldRunes) {
		nBackSpace += len(oldRunes) - offset
	}

	return newRunes[offset:], nBackSpace
}

func (e *IBusBambooEngine) SendBackSpace(n int, gen uint64) {
	// GOTV: guard revalidates the focus/field generation and password state
	// right before emitting, after the throttling waits. Sending a backspace
	// that started before a focus/password change would otherwise delete
	// characters from the field that now has focus — precisely the reported
	// "mất ký tự" bug.
	guard := func() bool {
		return e.isCurrentInputGen(gen) && !e.isPasswordField()
	}
	// Gtk/Qt apps have a serious sync issue with fake backspaces
	// and normal string committing, so we'll not commit right now
	// but delay until all the sent backspaces got processed.
	var now = time.Now()
	var delta = 50*1000*1000 - (now.UnixNano() - e.lastCommitText)
	if delta > 0 {
		time.Sleep(time.Duration(delta) * time.Nanosecond)
	}
	if !guard() {
		return
	}
	if e.checkInputMode(config.XTestFakeKeyEventIM) {
		e.setFakeBackspace(int32(n))
		var sleep = func() {
			var count = 0
			for e.getFakeBackspace() > 0 && count < 10 {
				time.Sleep(5 * time.Millisecond)
				count++
			}
		}
		log.Printf("Sendding %d backspace via XTestFakeKeyEvent\n", n)
		time.Sleep(10 * time.Millisecond)
		if !guard() {
			return
		}
		x11SendBackspace(n, 0)
		sleep()
		time.Sleep(time.Duration(n) * (10 + BACKSPACE_INTERVAL) * time.Millisecond)
	} else if e.checkInputMode(config.SurroundingTextIM) {
		time.Sleep(20 * time.Millisecond)
		log.Printf("Sendding %d backspace via SurroundingText\n", n)
		if !guard() {
			return
		}
		e.DeleteSurroundingText(-int32(n), uint32(n))
		time.Sleep(20 * time.Millisecond)
	} else if e.checkInputMode(config.ForwardAsCommitIM) {
		time.Sleep(20 * time.Millisecond)
		log.Printf("Sendding %d backspace via forwardAsCommitIM\n", n)
		for i := 0; i < n; i++ {
			if !guard() {
				return
			}
			e.ForwardKeyEvent(IBusBackSpace, XkBackspace-8, 0)
			e.ForwardKeyEvent(IBusBackSpace, XkBackspace-8, IBusReleaseMask)
		}
		time.Sleep(time.Duration(n) * (20 + BACKSPACE_INTERVAL) * time.Millisecond)
	} else if e.checkInputMode(config.ShiftLeftForwardingIM) {
		time.Sleep(30 * time.Millisecond)
		log.Printf("Sendding %d Shift+Left via shiftLeftForwardingIM\n", n)

		for i := 0; i < n; i++ {
			if !guard() {
				return
			}
			e.ForwardKeyEvent(IBusLeft, XkLeft-8, IBusShiftMask)
			e.ForwardKeyEvent(IBusLeft, XkLeft-8, IBusReleaseMask)
		}
		time.Sleep(time.Duration(n) * (30 + BACKSPACE_INTERVAL) * time.Millisecond)
	} else if e.checkInputMode(config.BackspaceForwardingIM) {
		time.Sleep(30 * time.Millisecond)
		log.Printf("Sendding %d backspace via backspaceForwardingIM\n", n)

		for i := 0; i < n; i++ {
			if !guard() {
				return
			}
			e.ForwardKeyEvent(IBusBackSpace, XkBackspace-8, 0)
			e.ForwardKeyEvent(IBusBackSpace, XkBackspace-8, IBusReleaseMask)
		}
		time.Sleep(time.Duration(n) * (30 + BACKSPACE_INTERVAL) * time.Millisecond)
	} else {
		fmt.Println("There's something wrong with wmClasses")
	}
}

func (e *IBusBambooEngine) resetFakeBackspace() {
	e.setFakeBackspace(0)
}

func (e *IBusBambooEngine) bsCommitText(rs []rune) {
	if len(rs) == 0 {
		return
	}
	if e.checkInputMode(config.ForwardAsCommitIM) {
		log.Println("Forward as commit", string(rs))
		for _, chr := range rs {
			var keyVal = vnSymMapping[chr]
			if keyVal == 0 {
				keyVal = uint32(chr)
			}
			e.ForwardKeyEvent(keyVal, 0, 0)
			e.ForwardKeyEvent(keyVal, 0, IBusReleaseMask)
		}
		time.Sleep(time.Duration(len(rs)) * 5 * time.Millisecond)
		return
	}
	e.commitText(string(rs))
}
