package tui

import (
	"path/filepath"

	tea "charm.land/bubbletea/v2"
	"github.com/DhanushSantosh/AgentComms/internal/store"
	"github.com/fsnotify/fsnotify"
)

type fsEventMsg struct{}

// EnableFileWatch starts watching the runtime's events directory so the
// TUI refreshes as soon as another CLI or MCP session commits a new signed
// event, instead of requiring a manual 'r'. It is opt-in and separate from
// New() so that tests constructing a Model directly never open a live OS
// file-watch handle (which would otherwise outlive the test and, on
// Windows, block t.TempDir's cleanup).
func (m *Model) EnableFileWatch() {
	w, err := fsnotify.NewWatcher()
	if err != nil {
		return
	}
	dir := filepath.Join(m.svc.Store.Root, store.Runtime, "events")
	if err := w.Add(dir); err != nil {
		_ = w.Close()
		return
	}
	m.watcher = w
}
func watchEventsCmd(w *fsnotify.Watcher) tea.Cmd {
	return func() tea.Msg {
		select {
		case _, ok := <-w.Events:
			if !ok {
				return nil
			}
			return fsEventMsg{}
		case _, ok := <-w.Errors:
			if !ok {
				return nil
			}
			return fsEventMsg{}
		}
	}
}
