package cliui

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"unicode"
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
