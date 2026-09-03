package tui

import (
	"strings"

	tea "charm.land/bubbletea/v2"
)

// Command-palette and form input handling: the palette command registry,
// updatePalette, updateForm, and their small helpers. Split out of
// model.go.

// paletteCommand is one named, directly-executable command the palette can
// offer, distinct from a bare view name (which only navigates). label is
// what's matched and displayed; aliases add extra phrasings ("create
// task" alongside "new task") that also match, without cluttering the
// displayed match list with duplicates of the same command.
type paletteCommand struct {
	label   string
	aliases []string
	view    string
	open    func(Model) (tea.Model, tea.Cmd)
}

func paletteCommands() []paletteCommand {
	return []paletteCommand{
		{label: "new task", aliases: []string{"create task"}, view: "Tasks",
			open: func(v Model) (tea.Model, tea.Cmd) { return v.openTaskForm() }},
		{label: "new agent", aliases: []string{"create agent", "register agent"}, view: "Agents",
			open: func(v Model) (tea.Model, tea.Cmd) { return v.openActionForm(agentRegisterForm, "agent.register", "") }},
		{label: "new message", aliases: []string{"create message"}, view: "Inbox",
			open: func(v Model) (tea.Model, tea.Cmd) { return v.openActionForm(messagePostForm, "message.post", "") }},
		{label: "new invocation", aliases: []string{"create invocation"}, view: "Invocations",
			open: func(v Model) (tea.Model, tea.Cmd) {
				return v.openActionForm(invocationRequestForm, "invocation.request", "")
			}},
		{label: "new runtime", aliases: []string{"create runtime"}, view: "Runtimes",
			open: func(v Model) (tea.Model, tea.Cmd) {
				return v.openActionForm(runtimeRegisterForm, "runtime.register", "")
			}},
		{label: "new document", aliases: []string{"create document"}, view: "Documents",
			open: func(v Model) (tea.Model, tea.Cmd) { return v.openActionForm(documentCreateForm, "document.create", "") }},
		{label: "new decision", aliases: []string{"create decision"}, view: "Contracts & decisions",
			open: func(v Model) (tea.Model, tea.Cmd) { return v.openActionForm(decisionCreateForm, "document.create", "") }},
		{label: "new artifact", aliases: []string{"add artifact"}, view: "Artifacts",
			open: func(v Model) (tea.Model, tea.Cmd) { return v.openActionForm(artifactAddForm, "artifact.add", "") }},
		{label: "new draft", aliases: []string{"save draft"}, view: "Drafts",
			open: func(v Model) (tea.Model, tea.Cmd) { return v.openActionForm(draftSaveForm, "draft.save", "") }},
		{label: "new environment key", aliases: []string{"set environment key"}, view: "Environment",
			open: func(v Model) (tea.Model, tea.Cmd) { return v.openActionForm(envSetForm, "env.set", "") }},
	}
}

// updatePalette handles every message while the command palette is open --
// checked before any other mode in Update(), so opening it from inside a
// focused row list (or Project settings) can never leak a keystroke into
// row actions or settings navigation underneath it -- confirmed live as a
// real, serious defect without that ordering: typed characters silently
// triggered row actions (suspend, revoke, delete, "n" opening an unrelated
// form) while the palette sat on screen showing an empty, frozen query box.
func (m Model) updatePalette(msg tea.Msg) (tea.Model, tea.Cmd) {
	if click, ok := msg.(tea.MouseClickMsg); ok {
		if click.Mouse().Button == tea.MouseLeft {
			p := colors(m.highContrast)
			if index, ok := m.paletteMatchAt(p, click.Mouse().X, click.Mouse().Y); ok {
				if matches := m.paletteMatches(); index < len(matches) {
					return matches[index].apply(m)
				}
			}
			// A click that missed every match row closes the palette
			// outright and re-dispatches the identical click through the
			// now-updated model -- with m.palette false, it falls through
			// to whatever that click would ordinarily do (a sidebar hub, a
			// hub tab, a click elsewhere). Clicking away from an open
			// palette is an unambiguous "go there now, forget the search"
			// the same way every other click in this app already wins over
			// whatever mode was active, not something that should be
			// silently swallowed just because the palette happened to be
			// open -- confirmed live as a real defect without this: the
			// sidebar/hub-tab click handling ran unconditionally, before
			// any mode check at all, so it silently navigated underneath a
			// palette that stayed visibly open on top, showing a frozen,
			// stale query over a screen that had already changed.
			m.palette, m.query = false, ""
			return m.Update(msg)
		}
		return m, nil
	}
	if _, ok := msg.(tea.MouseWheelMsg); ok {
		// Deliberately swallowed, not scrolled -- paletteMatches caps at
		// 6 rows, never taller than the panel, so there's nothing to
		// scroll here. Letting a wheel event reach whatever's underneath
		// while composing a query would be exactly the kind of
		// background-changes-while-typing surprise this whole fix
		// removes for clicks and keystrokes too.
		return m, nil
	}
	key, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return m, nil
	}
	switch k := key.String(); k {
	case "esc":
		m.palette = false
		m.query = ""
	case "enter":
		// An empty query doing nothing (rather than applying whatever
		// paletteMatches() lists first for an empty filter) matches the
		// original behavior: an accidental Enter before typing anything
		// should never silently open a form.
		if strings.TrimSpace(m.query) != "" {
			if matches := m.paletteMatches(); len(matches) > 0 {
				return matches[0].apply(m)
			}
		}
	case "backspace":
		if len(m.query) > 0 {
			m.query = m.query[:len(m.query)-1]
		}
	case "space":
		// bubbletea/ultraviolet's Key.String() reports the spacebar as
		// the literal word "space", never a single " " character --
		// confirmed straight from the vendored key.go source -- so the
		// len(k)==1 branch below can never see it. Without this case, no
		// multi-word command ("new task", "new environment key", ...)
		// could ever actually be typed.
		m.query += " "
	default:
		if len(k) == 1 {
			m.query += k
		}
	}
	return m, nil
}

func (m Model) openTaskForm() (tea.Model, tea.Cmd) {
	return m.openActionForm(taskCreateForm, "task.create", "")
}

func (m Model) updateForm(msg tea.Msg) (tea.Model, tea.Cmd) {
	if click, ok := msg.(tea.MouseClickMsg); ok {
		mouse := click.Mouse()
		if mouse.Button == tea.MouseLeft && len(m.inputs) > 0 {
			if field, ok := m.formFieldAtY(colors(m.highContrast), mouse.Y); ok && field != m.formFocus {
				m.inputs[m.formFocus].Blur()
				m.formFocus = field
				return m, m.inputs[m.formFocus].Focus()
			}
		}
		return m, nil
	}
	if key, ok := msg.(tea.KeyPressMsg); ok {
		if m.formSpec != nil && m.formFocus < len(m.formSpec.Fields) {
			if options := m.formSpec.Fields[m.formFocus].Options; len(options) > 0 {
				switch key.String() {
				case "left", "right", " ":
					current := cyclePickerOption(options, m.inputs[m.formFocus].Value(), key.String() == "left")
					m.inputs[m.formFocus].SetValue(current)
					return m, nil
				}
			}
		}
		switch key.String() {
		case "esc":
			m.form, m.inputs, m.err, m.formSpec = "", nil, nil, nil
			return m, nil
		case "tab", "down":
			m.inputs[m.formFocus].Blur()
			m.formFocus = (m.formFocus + 1) % len(m.inputs)
			return m, m.inputs[m.formFocus].Focus()
		case "shift+tab", "up":
			m.inputs[m.formFocus].Blur()
			m.formFocus = (m.formFocus - 1 + len(m.inputs)) % len(m.inputs)
			return m, m.inputs[m.formFocus].Focus()
		case "enter":
			if m.formFocus < len(m.inputs)-1 {
				m.inputs[m.formFocus].Blur()
				m.formFocus++
				return m, m.inputs[m.formFocus].Focus()
			}
			raw := make([]string, len(m.inputs))
			values := make([]string, len(m.inputs))
			for i := range m.inputs {
				raw[i] = m.inputs[i].Value()
				values[i] = strings.TrimSpace(raw[i])
			}
			for i, f := range m.formSpec.Fields {
				if f.Required && values[i] == "" {
					m.notice = "Complete every required field."
					return m, nil
				}
			}
			passphrase := ""
			if m.formSpec.CollectsPassphrase && len(raw) > 0 {
				passphrase = raw[len(raw)-1]
				values = values[:len(values)-1]
			}
			if m.formSpec.Dispatch != nil {
				return m.formSpec.Dispatch(m, values, passphrase)
			}
			payload, err := m.formSpec.Build(values)
			if err != nil {
				m.err = err
				return m, nil
			}
			id := m.formTaskID
			if m.formSpec.ResolveID != nil {
				id = m.formSpec.ResolveID(m.formTaskID, values)
			}
			typ := m.form
			if m.formSpec.ConfirmIf != nil {
				if ok, prompt := m.formSpec.ConfirmIf(payload); ok {
					m.form, m.inputs, m.formSpec = "", nil, nil
					m.confirm = &confirmState{prompt: prompt, typ: typ, id: id, payload: payload, passphrase: passphrase}
					return m, nil
				}
			}
			_, err = m.svc.ExecuteWithPassphrase(m.actor, typ, id, payload, passphrase)
			if err != nil {
				m.err = err
				return m, nil
			}
			m.form, m.inputs, m.err, m.formSpec = "", nil, nil, nil
			m.notice = "Applied " + typ + " to " + id
			m.refreshState()
			return m, nil
		}
	}
	commands := make([]tea.Cmd, len(m.inputs))
	for i := range m.inputs {
		if m.formSpec != nil && i < len(m.formSpec.Fields) && len(m.formSpec.Fields[i].Options) > 0 {
			// Picker fields only ever change via left/right cycling above;
			// typed characters are silently dropped rather than mutating a
			// value that must always be one of Options.
			continue
		}
		m.inputs[i], commands[i] = m.inputs[i].Update(msg)
	}
	return m, tea.Batch(commands...)
}

// cyclePickerOption returns the next (or, if backward, previous) value in
// options relative to current, wrapping around at either end. Falls back to
// options[0] if current doesn't match anything in the list (e.g. the field
// was just opened).
func cyclePickerOption(options []string, current string, backward bool) string {
	idx := 0
	for i, o := range options {
		if o == current {
			idx = i
			break
		}
	}
	if backward {
		idx = (idx - 1 + len(options)) % len(options)
	} else {
		idx = (idx + 1) % len(options)
	}
	return options[idx]
}

func splitCSV(value string) []string {
	var out []string
	for _, item := range strings.Split(value, ",") {
		if item = strings.TrimSpace(item); item != "" {
			out = append(out, item)
		}
	}
	return out
}
