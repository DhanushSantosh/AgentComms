package cliui

import "fmt"

// Timeline is a chronological human view for history and delivery evidence.
type Timeline struct {
	Title   string
	Entries []TimelineEntry
}

// TimelineEntry is one timestamped semantic event.
type TimelineEntry struct {
	Time   string
	Status Status
	Title  string
	Detail string
}

// RenderTimeline writes a stable chronological view with capability-aware
// markers and no terminal control sequences in plain/redirected output.
func (p Presenter) RenderTimeline(timeline Timeline) error {
	if err := p.renderTitle(Document{Title: timeline.Title, Status: StatusInfo}); err != nil {
		return err
	}
	if len(timeline.Entries) == 0 {
		_, err := fmt.Fprintln(p.Out, "\n(no events)")
		return err
	}
	if _, err := fmt.Fprintln(p.Out); err != nil {
		return err
	}
	for _, entry := range timeline.Entries {
		marker := "-"
		if p.Mode == ModeHuman && p.Capabilities.Interactive {
			marker = statusPrefix(entry.Status, p.Capabilities.Unicode)
		}
		if _, err := fmt.Fprintf(p.Out, "%s %s  %s", marker, safeText(entry.Time), safeText(entry.Title)); err != nil {
			return err
		}
		if entry.Detail != "" {
			if _, err := fmt.Fprintf(p.Out, "  %s", safeText(entry.Detail)); err != nil {
				return err
			}
		}
		if _, err := fmt.Fprintln(p.Out); err != nil {
			return err
		}
	}
	return nil
}
