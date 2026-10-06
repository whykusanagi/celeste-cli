package main

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/internal/fakeprovider"
	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/tui"
)

// countingWriter is a terminal that counts what the program writes.
type countingWriter struct {
	mu     sync.Mutex
	bytes  int
	writes int
}

func (w *countingWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.bytes += len(p)
	w.writes++
	return len(p), nil
}

func (w *countingWriter) counts() (int, int) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.bytes, w.writes
}

// askWatcher records whether the wrapped chat has its ask modal open.
type askWatcher struct {
	tea.Model
	open *atomic.Bool
}

func (a askWatcher) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	m, cmd := a.Model.Update(msg)
	a.Model = m
	a.open.Store(m.(tui.AppModel).DebugAskPromptActive())
	return a, cmd
}

// #401: the smoke's last frame was "ask executing 31.6s" for 17 minutes.
// The tick chain was alive (reproduced here with a fake provider and a real
// Bubble Tea program), but the TUI redrew ten times a second while the ask
// waited, ~2.2 KB/s; 31.6 s of that is ~68 KB, about the 64 KiB a pty
// holds while its reader is busy, after which the program's terminal write
// blocks until the reader drains it. While only a modal waits, the screen
// now redraws once a second: the timer still counts, at a tenth of the
// output.
func TestAskWaitRedrawsOncePerSecond(t *testing.T) {
	ask := fakeprovider.ToolCall{ID: "a", Name: "ask", Args: `{"question":"which?","options":[{"label":"x"},{"label":"y"}]}`}
	srv := fakeprovider.NewOpenAI(t, fakeprovider.Turn{Text: "Let me ask.", ToolCalls: []fakeprovider.ToolCall{ask}}, fakeprovider.Turn{Text: "final"})
	m, deps, _ := chatApp(t, srv)
	out := &countingWriter{}
	w := askWatcher{Model: m, open: new(atomic.Bool)}
	p := tea.NewProgram(w, tea.WithInput(nil), tea.WithOutput(out), tea.WithoutSignalHandler())
	deps.registry.SetAskFunc(askPrompt(p.Send))
	type rate struct{ bytes, writes int }
	got := make(chan rate, 1)
	go func() {
		defer p.Quit()
		p.Send(tea.WindowSizeMsg{Width: 160, Height: 50})
		p.Send(tui.SendMessageMsg{Content: "ask me"})
		deadline := time.Now().Add(20 * time.Second)
		for !w.open.Load() {
			if time.Now().After(deadline) {
				got <- rate{-1, -1}
				return
			}
			time.Sleep(20 * time.Millisecond)
		}
		time.Sleep(1500 * time.Millisecond) // the first slow tick replaces the fast one
		b0, w0 := out.counts()
		time.Sleep(3 * time.Second)
		b1, w1 := out.counts()
		got <- rate{b1 - b0, w1 - w0}
	}()
	if _, err := p.Run(); err != nil {
		t.Fatal(err)
	}
	r := <-got
	if r.bytes < 0 {
		t.Fatal("the ask modal never opened")
	}
	if r.writes < 1 {
		t.Fatalf("%d redraws in 3 s while the ask waited: the elapsed time stopped", r.writes)
	}
	// Ten a second wrote 30 frames here (~2.2 KB/s); a redraw a second
	// writes about 3, plus the git poll's every 3 s.
	if r.writes > 10 {
		t.Fatalf("%d redraws in 3 s while the ask waited (%d bytes/s), want about one a second", r.writes, r.bytes/3)
	}
}
