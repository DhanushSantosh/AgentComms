package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

func TestReleaseViewBoundsAcrossResizeEdges(t *testing.T) {
	s := newTestService(t)
	base, err := New(s, "owner")
	if err != nil {
		t.Fatal(err)
	}
	for _, size := range [][2]int{{1, 1}, {2, 2}, {12, 3}, {19, 7}, {20, 8}, {31, 13}, {59, 15}, {71, 23}, {103, 39}, {220, 70}} {
		for _, name := range views {
			m := base
			m.openView(name)
			updated, _ := m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
			m = updated.(Model)
			if m.width != size[0] || m.height != size[1] {
				t.Fatal("resize lost terminal dimensions")
			}
			for _, overlay := range []string{"normal", "palette", "confirm"} {
				candidate := m
				candidate.palette = overlay == "palette"
				if overlay == "confirm" {
					candidate.confirm = &confirmState{prompt: "Confirm a synthetic operation?"}
				}
				content := candidate.View().Content
				if got := lipgloss.Height(content); got > size[1] {
					t.Fatalf("%s/%s at %dx%d renders %d rows", name, overlay, size[0], size[1], got)
				}
				for _, line := range strings.Split(content, "\n") {
					if got := lipgloss.Width(line); got > size[0] {
						t.Fatalf("%s/%s at %dx%d renders %d columns", name, overlay, size[0], size[1], got)
					}
				}
			}
		}
	}
}

func TestReleaseViewRecoversAfterTinyOrZeroResize(t *testing.T) {
	s := newTestService(t)
	base, err := New(s, "owner")
	if err != nil {
		t.Fatal(err)
	}
	for _, size := range [][2]int{{0, 0}, {0, 24}, {80, 0}, {1, 1}} {
		m := base
		m.width, m.height = 80, 24
		before := m.View().Content
		updated, _ := m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
		m = updated.(Model)
		if (size[0] == 0 || size[1] == 0) && m.View().Content != "" {
			t.Fatal("zero-area terminal renders content")
		}
		updated, _ = m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
		m = updated.(Model)
		if after := m.View().Content; after != before {
			t.Fatal("resize clipped durable model state instead of only its view")
		}
	}
}
