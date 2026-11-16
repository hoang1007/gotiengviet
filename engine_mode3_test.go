package main

import (
	"sync"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/godbus/dbus/v5"

	"ibus-bamboo/config"

	"github.com/BambooEngine/bamboo-core"
)

// newMode3Engine builds an engine on BackspaceForwardingIM backed by a fake
// app buffer, so tests assert the text the application would actually see.
func newMode3Engine(t *testing.T) (*IBusBambooEngine, *fakeEngine) {
	t.Helper()
	fe := NewFakeEngine()
	fe.simulateAppBuffer = true
	cfg := config.DefaultCfg()
	cfg.DefaultInputMode = config.BackspaceForwardingIM
	inputMethod := bamboo.ParseInputMethod(cfg.InputMethodDefinitions, cfg.InputMethod)
	e := NewIbusBambooEngine("test", &cfg, fe, bamboo.NewEngine(inputMethod, cfg.Flags))
	e.shouldEnqueuKeyStrokes = true
	return e, fe
}

// testWorker replaces the package queue/handler with a private pair and runs
// the same keyPressCapturing loop production uses. stop() drains the queue and
// restores the globals.
type testWorker struct {
	ch         chan keyPressEvent
	done       chan struct{}
	oldChan    chan keyPressEvent
	oldHandler func(uint32, uint32, uint32, uint64)
	once       sync.Once
}

func startTestWorker(t *testing.T, e *IBusBambooEngine) *testWorker {
	t.Helper()
	w := &testWorker{
		ch:         make(chan keyPressEvent, 512),
		done:       make(chan struct{}),
		oldChan:    keyPressChan,
		oldHandler: keyPressHandler,
	}
	keyPressChan = w.ch
	keyPressHandler = e.keyPressForwardHandler
	go func() {
		defer close(w.done)
		keyPressCapturing(w.ch)
	}()
	t.Cleanup(w.stop)
	return w
}

func (w *testWorker) stop() {
	w.once.Do(func() {
		close(w.ch)
		<-w.done
		keyPressChan = w.oldChan
		keyPressHandler = w.oldHandler
	})
}

// typeQueued drives a string through the live queue/worker path and waits for
// every keystroke to be consumed.
func typeQueued(t *testing.T, e *IBusBambooEngine, s string) {
	t.Helper()
	w := startTestWorker(t, e)
	for _, c := range s {
		e.ProcessKeyEvent(uint32(c), uint32(c), 0)
	}
	w.stop()
}

// typeDirect runs the worker handler inline, keeping composition tests
// deterministic while exercising the same handler the worker calls.
func typeDirect(e *IBusBambooEngine, s string) {
	gen := e.currentInputGen()
	for _, c := range s {
		e.keyPressForwardHandler(uint32(c), uint32(c), 0, gen)
	}
}

func TestSuffixPatch(t *testing.T) {
	cases := []struct {
		old, new   string
		backspaces int
		insert     string
	}{
		{"", "đề", 0, "đề"},
		{"d", "đề", 1, "đề"},
		{"đê", "đề", 1, "ề"},
		{"vân", "vấn đề", 2, "ấn đề"},
		{"abc", "abc", 0, ""},
		{"abc", "abd", 1, "d"},
		{"", "", 0, ""},
	}
	for _, tc := range cases {
		gotBackspaces, gotInsert := suffixPatch(tc.old, tc.new)
		if gotBackspaces != tc.backspaces || gotInsert != tc.insert {
			t.Errorf("suffixPatch(%q, %q) = (%d, %q), want (%d, %q)",
				tc.old, tc.new, gotBackspaces, gotInsert, tc.backspaces, tc.insert)
		}
	}
}

func TestBackspaceForwardingComposition(t *testing.T) {
	e, fe := newMode3Engine(t)
	cases := []struct{ in, want string }{
		{"thayas ", "thấy "},
		{"maats ", "mất "},
		{"truwowng ", "trương "},
		{"duowidro ", "đuổi "},
		{"hoangf ", "hoàng "},
		{"bawngf ", "bằng "},
		{"as ", "á "},
		{"ddeef ", "đề "},
		{"ddepj ", "đẹp "},
		{"caay. Tooi ", "cây. Tôi "},
		{"caay, Tooi ", "cây, Tôi "},
	}
	for _, tc := range cases {
		fe.resetAppBuffer()
		typeDirect(e, tc.in)
		if got := fe.appSnapshot(); got != tc.want {
			t.Errorf("type %q: got %q, want %q (commits: %v)",
				tc.in, got, tc.want, fe.commitLogSnapshot())
		}
	}
}

func TestBackspaceForwardingQueuedBurst(t *testing.T) {
	e, fe := newMode3Engine(t)
	const input = "ngaau hoom nay troiwf ddepj. Tooi thayas nos! "
	const want = "ngâu hôm nay trời đẹp. Tôi thấy nó! "
	typeQueued(t, e, input)
	if got := fe.appSnapshot(); got != want {
		t.Fatalf("burst: got %q, want %q (commits: %v)",
			got, want, fe.commitLogSnapshot())
	}
}

// TestSurroundingTextConcurrentWithWorker keeps a worker consuming keystrokes
// while the application pushes surrounding text back at the engine — the live
// Chromium/WebKit pattern. Run with -race; the invariant asserted here is that
// the engine survives the churn and leaves a valid app buffer.
func TestSurroundingTextConcurrentWithWorker(t *testing.T) {
	e, fe := newMode3Engine(t)
	typeQueued(t, e, "ngaau ")
	if got := fe.appSnapshot(); got != "ngâu " {
		t.Fatalf("baseline: got %q, want %q", got, "ngâu ")
	}

	w := startTestWorker(t, e)
	stop := make(chan struct{})
	sutDone := make(chan struct{})
	go func() {
		defer close(sutDone)
		payload := dbus.MakeVariant([]interface{}{uint32(0), uint32(0), "ngâu"})
		for {
			select {
			case <-stop:
				return
			default:
			}
			e.Lock()
			e.isSurroundingTextReady = true
			e.Unlock()
			e.SetSurroundingText(payload, 4, 4)
			time.Sleep(time.Millisecond)
		}
	}()

	for i := 0; i < 20; i++ {
		for _, c := range "ngaau " {
			e.ProcessKeyEvent(uint32(c), uint32(c), 0)
		}
	}
	close(stop)
	<-sutDone
	w.stop()

	if got := fe.appSnapshot(); !utf8.ValidString(got) {
		t.Errorf("app buffer is not valid UTF-8: %q", got)
	}
}
