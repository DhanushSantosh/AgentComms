package main

import (
	"io"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
)

// resizeProbeModel is a throwaway tea.Model whose only purpose is to prove
// encodeWindowSizeEvent's bytes actually survive bubbletea/ultraviolet's real
// input-parsing path -- the same path tui.Run's program uses -- and arrive as
// a tea.WindowSizeMsg with the expected width/height. This exercises no
// js-specific code: it is the identical byte-level contract wasm_main.go's
// jsInputBuffer feeds tui.Run through, verified here on the host platform
// with a plain io.Pipe standing in for the JS-fed buffer.
type resizeProbeModel struct {
	got chan tea.WindowSizeMsg
}

func (m resizeProbeModel) Init() tea.Cmd { return nil }

func (m resizeProbeModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if wsm, ok := msg.(tea.WindowSizeMsg); ok {
		m.got <- wsm
		// bubbletea/v2 also sends a synthetic WindowSizeMsg{0,0} at program
		// start (there is no real tty to size from) before our injected
		// escape sequence is even parsed -- only quit once we see the
		// non-zero size that our encoded bytes produced.
		if wsm.Width != 0 || wsm.Height != 0 {
			return m, tea.Quit
		}
	}
	return m, nil
}

func (m resizeProbeModel) View() tea.View { return tea.NewView("") }

func TestEncodeWindowSizeEventRoundTripsThroughRealParser(t *testing.T) {
	const wantCols, wantRows = 120, 40

	pr, pw := io.Pipe()
	got := make(chan tea.WindowSizeMsg, 4)
	p := tea.NewProgram(resizeProbeModel{got: got},
		tea.WithInput(pr),
		tea.WithOutput(io.Discard),
		tea.WithoutRenderer(),
		tea.WithoutSignalHandler(),
	)

	runErrCh := make(chan error, 1)
	go func() { _, err := p.Run(); runErrCh <- err }()

	go func() { _, _ = pw.Write(encodeWindowSizeEvent(wantCols, wantRows)) }()

	deadline := time.After(5 * time.Second)
	matched := false
	for !matched {
		select {
		case wsm := <-got:
			if wsm.Width == wantCols && wsm.Height == wantRows {
				matched = true
			}
		case <-deadline:
			t.Fatal("timed out waiting for the encoded tea.WindowSizeMsg from the real ultraviolet parser")
		}
	}

	_ = pw.Close()
	select {
	case err := <-runErrCh:
		if err != nil {
			t.Fatalf("p.Run() returned error: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for tea.Program to exit after Quit")
	}
}
