package main

import (
	"testing"
	"time"

	"ibus-bamboo/config"
)

// pressKey feeds one key through the live worker and waits for it to finish.
// Enqueued keys are ack'd with an explicit FIFO barrier (polling
// len(keyPressChan)==0 && workerBusy==0 is racy: the worker clears the channel
// before setting workerBusy), then a preeditorMu handshake closes the
// ack-before-final-commit gap of the batch drain.
func pressKey(t *testing.T, e *IBusBambooEngine, keyVal, keyCode, state uint32) (handled bool) {
	t.Helper()
	handled, _ = e.ProcessKeyEvent(keyVal, keyCode, state)
	if !handled {
		return handled
	}
	barrier := make(chan struct{})
	select {
	case keyPressChan <- keyPressEvent{barrier: barrier}:
	case <-time.After(3 * time.Second):
		t.Fatal("pressKey: barrier enqueue timed out")
	}
	select {
	case <-barrier:
	case <-time.After(3 * time.Second):
		t.Fatal("pressKey: worker did not drain the key within 3s")
	}
	// The batch drain (updatePreviousTextInBatch) may ack the barrier before its
	// final applySuffixPatch commit lands; keyPressHandler holds preeditorMu
	// through that commit, so acquiring the lock afterwards gives a hard
	// happens-before edge before the caller inspects the preeditor/app buffer.
	e.preeditorMu.Lock()
	e.preeditorMu.Unlock()
	return handled
}

// TestMode3ShortcutPassesThroughIdle: keys the IME can never use for Vietnamese
// text (Ctrl/Alt/Super chords, function/navigation keys) must return unhandled
// and never be re-injected via ForwardKeyEvent.
func TestMode3ShortcutPassesThroughIdle(t *testing.T) {
	shortcuts := []struct {
		name   string
		keyVal uint32
		state  uint32
	}{
		{"ctrl+w", 'w', IBusControlMask},
		{"ctrl+c", 'c', IBusControlMask},
		{"ctrl+v", 'v', IBusControlMask},
		{"win+space", IBusSpace, IBusMod4Mask},
		{"alt+tab", IBusTab, IBusMod1Mask},
		{"f5", 0xffc2, 0}, // XK_F5
		{"return", IBusReturn, 0},
		{"escape", IBusEscape, 0},
		{"delete", 0xffff, 0},
		{"insert", IBusInsert, 0},
		{"ctrl+backspace", IBusBackSpace, IBusControlMask},
		{"alt+backspace", IBusBackSpace, IBusMod1Mask},
		{"ctrl+tab", IBusTab, IBusControlMask},
	}
	for _, tc := range shortcuts {
		t.Run(tc.name, func(t *testing.T) {
			e, fe := newMode3Engine(t)
			w := startTestWorker(t, e)
			handled, _ := e.ProcessKeyEvent(tc.keyVal, tc.keyVal, tc.state)
			w.stop()
			if handled {
				t.Fatalf("%s must not be swallowed by the engine (handled=true)", tc.name)
			}
			if fe.forwardKeyEvent != [3]uint32{} {
				t.Fatalf("%s must not be re-injected via ForwardKeyEvent: %v", tc.name, fe.forwardKeyEvent)
			}
			if got := fe.appSnapshot(); got != "" {
				t.Fatalf("%s must not type a bare character into the app, buffer=%q", tc.name, got)
			}
		})
	}
}

// TestMode3ShortcutEndsInFlightWord: a shortcut pressed mid-composition flushes
// the in-flight word, then passes through natively.
func TestMode3ShortcutEndsInFlightWord(t *testing.T) {
	e, fe := newMode3Engine(t)
	w := startTestWorker(t, e)
	for _, c := range "thay" {
		pressKey(t, e, uint32(c), uint32(c), 0)
	}
	if got := e.getRawKeyLen(); got == 0 {
		t.Fatalf("expected an in-flight word before the shortcut")
	}
	handled := pressKey(t, e, 'w', 'w', IBusControlMask)
	if handled {
		t.Fatalf("shortcut mid-word must pass through, got handled=true")
	}
	if fe.forwardKeyEvent == [3]uint32{'w', 'w', IBusControlMask} {
		t.Fatalf("shortcut must not be re-injected as an ordinary key: %v", fe.forwardKeyEvent)
	}
	if got := e.getRawKeyLen(); got != 0 {
		t.Fatalf("shortcut must end (flush) the in-flight word, rawKeyLen=%d", got)
	}
	w.stop()
}

// TestMode3ChordIntactAfterFocusStorm reproduces the reported scenario: Ctrl+W,
// then Win+Space (GNOME input-source switch) fires a focus/generation storm,
// then Ctrl+W must still work.
func TestMode3ChordIntactAfterFocusStorm(t *testing.T) {
	e, fe := newMode3Engine(t)
	w := startTestWorker(t, e)

	// (1) baseline: Ctrl+W passes through natively.
	if h := pressKey(t, e, 'w', 'w', IBusControlMask); h {
		t.Fatalf("baseline Ctrl+W must pass through")
	}
	if got := fe.appSnapshot(); got != "" {
		t.Fatalf("baseline Ctrl+W typed a bare char? buffer=%q", got)
	}

	// (2) Win+Space (Super_L, state 64 / IBusMod4Mask), as in the live log.
	pressKey(t, e, 0xffeb, 0xffeb, 0)
	pressKey(t, e, IBusSpace, IBusSpace, IBusMod4Mask)

	// (3) the GNOME storm (Reset/Focus/SetContentType burst).
	for i := 0; i < 4; i++ {
		e.invalidatePendingKeys()
	}
	e.SetContentType(IBusInputPurposeEmail, 64)
	e.invalidatePendingKeys()
	e.SetContentType(IBusInputPurposeNormal, 64)
	e.invalidatePendingKeys()

	// (4) Ctrl+W again.
	if h := pressKey(t, e, 'w', 'w', IBusControlMask); h {
		t.Fatalf("Ctrl+W after Win+Space must still pass through")
	}
	if got := fe.appSnapshot(); got != "" {
		t.Fatalf("Ctrl+W after Win+Space must not type a bare 'w', buffer=%q (commits=%v)",
			got, fe.commitLogSnapshot())
	}
	w.stop()
}

// TestMode3ShortcutDoesNotRegressComposition: Vietnamese typing (including a
// word-break rewrite) must keep working across interspersed shortcuts.
func TestMode3ShortcutDoesNotRegressComposition(t *testing.T) {
	e, fe := newMode3Engine(t)
	w := startTestWorker(t, e)
	for _, c := range "thayas " {
		pressKey(t, e, uint32(c), uint32(c), 0)
	}
	pressKey(t, e, 'w', 'w', IBusControlMask)
	pressKey(t, e, IBusSpace, IBusSpace, IBusMod4Mask)
	for _, c := range "ngaau " {
		pressKey(t, e, uint32(c), uint32(c), 0)
	}
	w.stop()
	if got := fe.appSnapshot(); got != "thấy ngâu " {
		t.Fatalf("composition regressed around shortcuts: got %q, want %q", got, "thấy ngâu ")
	}
}

// TestMode3QueuedMacroTabStillExpands: "vn" then Tab before the worker drains
// must still expand the macro — Tab only becomes valid once the queued macro
// text is composed, so validity is checked again after the barrier.
func TestMode3QueuedMacroTabStillExpands(t *testing.T) {
	e, fe := newMode3Engine(t)
	e.config.IBflags |= config.IBmacroEnabled
	e.config.IBflags &^= config.IBautoCapitalizeMacro // deliver macro text verbatim
	e.macroTable = &MacroTable{mTable: map[string]string{"vn": "EXPANDED"}}
	w := startTestWorker(t, e)

	// Enqueue the macro text fast, without waiting for the worker to drain it.
	e.ProcessKeyEvent('v', 'v', 0)
	e.ProcessKeyEvent('n', 'n', 0)

	handled := pressKey(t, e, IBusTab, IBusTab, 0)
	if !handled {
		t.Fatalf("Tab after queued macro text must be consumed (expand the macro), got handled=false")
	}
	if got := fe.appSnapshot(); got != "EXPANDED" {
		t.Fatalf("Tab must expand the queued macro, app buffer=%q (commits=%v)", got, fe.commitLogSnapshot())
	}
	w.stop()
}

// TestMode3CtrlTabMacroNotExpanded: Ctrl+Tab must always pass through natively
// even when a macro is armed — a control chord can never be a macro trigger.
func TestMode3CtrlTabMacroNotExpanded(t *testing.T) {
	e, fe := newMode3Engine(t)
	e.config.IBflags |= config.IBmacroEnabled
	e.macroTable = &MacroTable{mTable: map[string]string{"vn": "EXPANDED"}}
	w := startTestWorker(t, e)

	for _, c := range "vn" {
		pressKey(t, e, uint32(c), uint32(c), 0)
	}
	if h := pressKey(t, e, IBusTab, IBusTab, IBusControlMask); h {
		t.Fatalf("Ctrl+Tab must pass through natively (not expand the macro), got handled=true")
	}
	if fe.forwardKeyEvent == [3]uint32{IBusTab, IBusTab, IBusControlMask} {
		t.Fatalf("Ctrl+Tab must not be re-injected as an ordinary key: %v", fe.forwardKeyEvent)
	}
	w.stop()
}

// TestMode3WordBreakStillHandled: space/punctuation end the current Vietnamese
// word, so they must still be consumed by the engine, not passed through.
func TestMode3WordBreakStillHandled(t *testing.T) {
	e, _ := newMode3Engine(t)
	w := startTestWorker(t, e)
	for _, c := range "thay" {
		pressKey(t, e, uint32(c), uint32(c), 0)
	}
	for _, c := range []uint32{IBusSpace, '.', ',', '?'} {
		if h := pressKey(t, e, c, c, 0); !h {
			t.Errorf("key %q (word break) must still be consumed by the engine, got handled=false", rune(c))
		}
	}
	w.stop()
}
