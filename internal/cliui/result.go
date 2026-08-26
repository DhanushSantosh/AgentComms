package cliui

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"unicode"

	"github.com/charmbracelet/x/ansi"
)

// RenderResult renders an arbitrary bounded command result through the safe
// human presentation boundary. It is the migration fallback for command
// shapes that do not yet have a purpose-built Document or Table.
func (p Presenter) RenderResult(command string, value, delivery any) error {
	normalized, err := normalizeResult(value)
	if err != nil {
		return err
	}
	if err := p.renderTitle(Document{Title: humanLabel(command), Status: StatusSuccess}); err != nil {
		return err
	}
	if normalized != nil {
		if _, err := fmt.Fprintln(p.Out); err != nil {
			return err
		}
		if err := p.renderTree(normalized, 0); err != nil {
			return err
		}
	}
	if delivery == nil {
		return nil
	}
	normalizedDelivery, err := normalizeResult(delivery)
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintln(p.Out, "\nDelivery"); err != nil {
		return err
	}
	return p.renderTree(normalizedDelivery, 1)
}

// RenderDetails appends the complete secondary result structure below an
// intentional summary without changing machine output contracts.
func (p Presenter) RenderDetails(value any) error {
	return p.RenderSection("Details", value)
}

// RenderSection appends a named structured section.
func (p Presenter) RenderSection(title string, value any) error {
	normalized, err := normalizeResult(value)
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintln(p.Out, "\n"+safeText(title)); err != nil {
		return err
	}
	return p.renderTree(normalized, 1)
}

// RenderWarning writes one sanitized diagnostic line.
func (p Presenter) RenderWarning(message string) error {
	prefix := "warning: "
	if p.Mode == ModeHuman && p.Capabilities.Interactive && p.Capabilities.Color {
		prefix = "\x1b[33mwarning:\x1b[0m "
	}
	_, err := fmt.Fprintln(p.Out, prefix+safeText(message))
	return err
}

// RenderError writes a concise classified failure and optional recovery hint.
func (p Presenter) RenderError(code, message, hint string) error {
	prefix := "error"
	if p.Mode == ModeHuman && p.Capabilities.Interactive && p.Capabilities.Color {
		prefix = "\x1b[31merror\x1b[0m"
	}
	if code != "" {
		prefix += " [" + safeText(code) + "]"
	}
	if _, err := fmt.Fprintln(p.Out, prefix+": "+safeText(message)); err != nil {
		return err
	}
	if hint != "" {
		_, err := fmt.Fprintln(p.Out, "hint: "+safeText(hint))
		return err
	}
	return nil
}

// RenderText renders intentional multiline content such as generated agent
// instructions while stripping terminal escape sequences and unsafe controls.
func (p Presenter) RenderText(title, value string) error {
	if err := p.renderTitle(Document{Title: title, Status: StatusInfo}); err != nil {
		return err
	}
	_, err := fmt.Fprintln(p.Out, "\n"+safeMultiline(value))
	return err
}

func safeMultiline(value string) string {
	value = ansi.Strip(value)
	return strings.Map(func(character rune) rune {
		if character == '\n' || character == '\t' {
			return character
		}
		if character < 0x20 || character == 0x7f {
			return -1
		}
		return character
	}, value)
}

func normalizeResult(value any) (any, error) {
	if value == nil {
		return nil, nil
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return nil, fmt.Errorf("prepare CLI result: %w", err)
	}
	var normalized any
	if err := json.Unmarshal(raw, &normalized); err != nil {
		return nil, fmt.Errorf("prepare CLI result: %w", err)
	}
	return normalized, nil
}

func (p Presenter) renderTree(value any, depth int) error {
	indent := strings.Repeat("  ", depth)
	switch typed := value.(type) {
	case map[string]any:
		keys := make([]string, 0, len(typed))
		for key := range typed {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			label := safeText(humanLabel(key))
			child := typed[key]
			switch nested := child.(type) {
			case map[string]any:
				if _, err := fmt.Fprintf(p.Out, "%s%s\n", indent, label); err != nil {
					return err
				}
				if err := p.renderTree(nested, depth+1); err != nil {
					return err
				}
			case []any:
				if _, err := fmt.Fprintf(p.Out, "%s%s (%d)\n", indent, label, len(nested)); err != nil {
					return err
				}
				if err := p.renderTree(nested, depth+1); err != nil {
					return err
				}
			default:
				if _, err := fmt.Fprintf(p.Out, "%s%s  %s\n", indent, label, scalarText(nested)); err != nil {
					return err
				}
			}
		}
	case []any:
		if len(typed) == 0 {
			_, err := fmt.Fprintf(p.Out, "%s(none)\n", indent)
			return err
		}
		for _, item := range typed {
			switch nested := item.(type) {
			case map[string]any, []any:
				if _, err := fmt.Fprintf(p.Out, "%s-\n", indent); err != nil {
					return err
				}
				if err := p.renderTree(nested, depth+1); err != nil {
					return err
				}
			default:
				if _, err := fmt.Fprintf(p.Out, "%s- %s\n", indent, scalarText(nested)); err != nil {
					return err
				}
			}
		}
	default:
		_, err := fmt.Fprintf(p.Out, "%s%s\n", indent, scalarText(typed))
		return err
	}
	return nil
}

func scalarText(value any) string {
	switch typed := value.(type) {
	case nil:
		return "—"
	case string:
		return safeText(typed)
	case bool:
		if typed {
			return "yes"
		}
		return "no"
	default:
		return safeText(fmt.Sprint(typed))
	}
}

func humanLabel(value string) string {
	value = strings.NewReplacer(".", " ", "_", " ", "-", " ").Replace(value)
	value = strings.Join(strings.Fields(value), " ")
	if value == "" {
		return "Result"
	}
	runes := []rune(strings.ToLower(value))
	runes[0] = unicode.ToUpper(runes[0])
	return string(runes)
}
