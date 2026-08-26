package tui

import (
	"errors"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/DhanushSantosh/AgentComms/internal/identity"
	"github.com/DhanushSantosh/AgentComms/internal/model"
)

var settingsSections = []struct {
	name, label, summary string
}{
	{"Project policy", "POLICY", "Leases, review gates, retention, and payload boundaries."},
	{"Agents & access", "PEOPLE", "Principals, roles, scopes, and invocation trust."},
	{"Agent runtimes", "RUNTIME", "Connectors, capacity, health, drain, and revocation."},
	{"Authority & data", "SYSTEM", "Authority mode, consistency, cache, and internal storage."},
	{"Environment", "ENV", "Project-scoped key/value configuration."},
	{"Interface", "LOCAL", "Per-user display preferences; never written to project history."},
}

func (m Model) updateSettings(message tea.Msg) (tea.Model, tea.Cmd) {
	if wheel, ok := message.(tea.MouseWheelMsg); ok {
		switch wheel.Button {
		case tea.MouseWheelUp:
			if m.settingsCursor > 0 {
				m.settingsCursor--
			}
		case tea.MouseWheelDown:
			if m.settingsCursor < len(settingsSections)-1 {
				m.settingsCursor++
			}
		}
		return m, nil
	}
	if click, ok := message.(tea.MouseClickMsg); ok {
		mouse := click.Mouse()
		if mouse.Button == tea.MouseLeft {
			if index, ok := m.settingsSectionAt(colors(m.highContrast), mouse.X, mouse.Y); ok {
				double := m.isDoubleClick(mouse.X, mouse.Y, time.Now())
				m.settingsCursor = index
				if double {
					return m.enterSettingsDomain(index)
				}
			}
		}
		return m, nil
	}
	key, ok := message.(tea.KeyPressMsg)
	if !ok {
		return m, nil
	}
	switch key.String() {
	case "esc", "left":
		m.settingsFocus = false
	case "q", "ctrl+c":
		return m, tea.Quit
	case "up", "k":
		if m.settingsCursor > 0 {
			m.settingsCursor--
		}
	case "down", "j":
		if m.settingsCursor < len(settingsSections)-1 {
			m.settingsCursor++
		}
	case "e", "enter":
		return m.enterSettingsDomain(m.settingsCursor)
	case "g":
		m.openView("Agents")
		m.settingsFocus, m.rowFocus = false, true
		m.agentList.Refresh(m.state, m.actor)
	case "r":
		m.openView("Runtimes")
		m.settingsFocus, m.rowFocus = false, true
		m.runtimeList.Refresh(m.state, m.actor)
	case "h":
		m.toggleTheme()
	case "?":
		m.notice = "↑/↓ choose domain · e/enter manage · g agents · r runtimes · h contrast · esc navigation"
	}
	return m, nil
}

// enterSettingsDomain runs whatever "e"/"enter" (keyboard) or a double-click
// (mouse) both mean for the given domain index -- opening its form, moving
// into its own row-focused view, or toggling the local theme. Shared so
// the two input paths can never disagree about what a domain does.
func (m Model) enterSettingsDomain(index int) (tea.Model, tea.Cmd) {
	switch index {
	case 0:
		return m.openProjectSettingsForm()
	case 1:
		m.openView("Agents")
		m.settingsFocus, m.rowFocus = false, true
		m.agentList.Refresh(m.state, m.actor)
	case 2:
		m.openView("Runtimes")
		m.settingsFocus, m.rowFocus = false, true
		m.runtimeList.Refresh(m.state, m.actor)
	case 3:
		return m.openDangerZoneForm()
	case 4:
		m.openView("Environment")
		m.settingsFocus, m.rowFocus = false, true
		m.envList.Refresh(m.state, m.actor)
	case 5:
		m.toggleTheme()
	}
	return m, nil
}

// dangerZoneForm is RFC 0020's TUI entry point for permanent project
// deletion -- reached only through "Authority & data" (settingsSections
// index 3), distinct from every other settings domain in what it does, not
// just how it looks. The two typed fields (project directory name,
// elevated-key passphrase) ARE the confirmation; there is no separate
// confirm dialog on top, matching the CLI's own single confirmation step.
// The confirmation asks for the project's directory name, not its
// internal ID -- see Service.DeleteProject's own doc comment for why: the
// ID is an opaque UUID nobody has memorized, while the directory name is
// something already known without looking anything up and just as
// effective at catching "right person, wrong project."
var dangerZoneForm = &ActionForm{
	Title: "Delete this project permanently",
	Hint: "OWNER-only. Deletes the local runtime, and in service mode this project's entire " +
		"remote data too -- every other member's access ends with it. There is no backup; this " +
		"cannot be undone. See docs/rfcs/0020-elevated-key-gated-project-deletion.md.",
	Fields: []FormField{
		{Label: "Type the project directory name to confirm", Required: true},
		{Label: "Elevated-key passphrase", Mask: true, Required: true},
	},
	CollectsPassphrase: true,
	// A plain Build+ExecuteWithPassphrase can't be used here at all --
	// DeleteProject isn't a state-machine transition, it destroys the
	// state machine's own storage. Dispatch is the only escape hatch that
	// fits.
	Dispatch: func(m Model, v []string, passphrase string) (tea.Model, tea.Cmd) {
		result, err := m.svc.DeleteProject(m.actor, passphrase, v[0])
		if err != nil {
			m.err = err
			return m, nil
		}
		// The project this Model was built against no longer exists --
		// there is no view left to return to. Quit exactly like "q", but
		// leave a final message Run's caller prints once the alt screen
		// has actually torn down (m.notice would just vanish with it).
		m.form, m.inputs, m.formSpec = "", nil, nil
		m.exitNotice = fmt.Sprintf("Project %s permanently deleted.", result.ProjectID)
		return m, tea.Quit
	},
}

func (m Model) openDangerZoneForm() (tea.Model, tea.Cmd) {
	next, cmd := m.openActionForm(dangerZoneForm, "project.delete", m.projectID)
	opened := next.(Model)
	// Shown as a placeholder, never pre-filled as a value: pre-filling it
	// would let Enter confirm the single most destructive action in the
	// system without the operator having typed anything at all, defeating
	// the entire point of asking.
	if m.svc != nil && len(opened.inputs) > 0 {
		opened.inputs[0].Placeholder = filepath.Base(m.svc.Store.Root)
	}
	return opened, cmd
}

func (m *Model) toggleTheme() {
	m.highContrast = !m.highContrast
	theme := "auto"
	if m.highContrast {
		theme = "high-contrast"
	}
	if config, err := identity.LoadUserConfig(); err == nil {
		config.Theme = theme
		if err = identity.SaveUserConfig(config); err != nil {
			m.err = err
			return
		}
	}
	m.err = nil
	m.notice = "Local interface theme set to " + theme
}

func (m Model) openProjectSettingsForm() (tea.Model, tea.Cmd) {
	current := model.EffectiveProjectSettings(m.state.ProjectSettings)
	spec := &ActionForm{
		Title: "Signed project policy change",
		Hint:  "Owner or orchestrator authority required. Saving appends an auditable project.settings.update event.",
		Fields: []FormField{
			{Label: "Default lease", Placeholder: current.DefaultLease, Required: true},
			{Label: "Stale grace", Placeholder: current.StaleGrace, Required: true},
			{Label: "Active retention", Placeholder: current.ActiveRetention, Required: true},
			{Label: "Summary characters", Placeholder: strconv.Itoa(current.SummaryLimit), Required: true},
			{Label: "Artifact limit MiB", Placeholder: strconv.FormatInt(current.ArtifactLimitBytes/(1024*1024), 10), Required: true},
			{Label: "Require review", Placeholder: strconv.FormatBool(current.RequireReview), Required: true},
		},
		Build: func(values []string) (any, error) {
			summaryLimit, err := strconv.Atoi(values[3])
			if err != nil {
				return nil, errors.New("summary characters must be a whole number")
			}
			artifactMiB, err := strconv.ParseInt(values[4], 10, 64)
			if err != nil || artifactMiB < 1 {
				return nil, errors.New("artifact limit MiB must be a positive whole number")
			}
			requireReview, err := strconv.ParseBool(strings.ToLower(values[5]))
			if err != nil {
				return nil, errors.New("require review must be true or false")
			}
			return model.ProjectSettingsUpdated{
				DefaultLease: values[0], StaleGrace: values[1], ActiveRetention: values[2],
				SummaryLimit: summaryLimit, ArtifactLimitBytes: artifactMiB * 1024 * 1024,
				RequireReview: requireReview,
			}, nil
		},
		ConfirmIf: func(any) (bool, string) {
			return true, "Sign and publish this project-wide policy change?"
		},
	}
	next, command := m.openActionForm(spec, "project.settings.update", "project")
	opened := next.(Model)
	defaults := []string{
		current.DefaultLease, current.StaleGrace, current.ActiveRetention,
		strconv.Itoa(current.SummaryLimit), strconv.FormatInt(current.ArtifactLimitBytes/(1024*1024), 10),
		strconv.FormatBool(current.RequireReview),
	}
	for index := range defaults {
		opened.inputs[index].SetValue(defaults[index])
	}
	return opened, command
}

func (m Model) projectSettings(p palette, width, height int) string {
	domainWidth := max(24, min(28, width/4))
	mainWidth := max(34, width-domainWidth-3)
	domains := m.settingsDomainRail(p, domainWidth)
	control := m.settingsControl(p, mainWidth)
	var content string
	if width >= 72 {
		content = lipgloss.JoinHorizontal(lipgloss.Top, domains, "  ", control)
	} else {
		content = m.settingsSelectedDomain(p) + "\n\n" + m.settingsControl(p, width)
	}
	footerParts := []string{
		lipgloss.NewStyle().Foreground(p.cyan).Bold(true).Render("[↑/↓]") + " " + lipgloss.NewStyle().Foreground(p.muted).Render("domain"),
		lipgloss.NewStyle().Foreground(p.cyan).Bold(true).Render("[e/enter]") + " " + lipgloss.NewStyle().Foreground(p.muted).Render("manage"),
		lipgloss.NewStyle().Foreground(p.cyan).Bold(true).Render("[g]") + " " + lipgloss.NewStyle().Foreground(p.muted).Render("agents"),
		lipgloss.NewStyle().Foreground(p.cyan).Bold(true).Render("[r]") + " " + lipgloss.NewStyle().Foreground(p.muted).Render("runtimes"),
		lipgloss.NewStyle().Foreground(p.cyan).Bold(true).Render("[h]") + " " + lipgloss.NewStyle().Foreground(p.muted).Render("contrast"),
		lipgloss.NewStyle().Foreground(p.cyan).Bold(true).Render("[esc]") + " " + lipgloss.NewStyle().Foreground(p.muted).Render("back"),
	}
	parts := []string{}
	for _, part := range footerParts {
		candidate := strings.Join(append(parts, part), " · ")
		if lipgloss.Width(candidate) <= width {
			parts = append(parts, part)
		} else {
			break
		}
	}
	footer := strings.Join(parts, " · ")
	return content + "\n\n" + footer
}

func (m Model) settingsDomainRail(p palette, width int) string {
	rows := []string{lipgloss.NewStyle().Foreground(p.muted).Bold(true).Render("CONTROL DOMAINS"), ""}
	for index, section := range settingsSections {
		marker := "  "
		style := lipgloss.NewStyle().Foreground(p.muted)
		if index == m.settingsCursor {
			marker = "▌ "
			style = style.Foreground(p.cyan).Bold(true)
		}
		rows = append(rows, style.Render(marker+section.name))
	}
	return lipgloss.NewStyle().Width(width).Border(lipgloss.NormalBorder()).BorderForeground(p.muted).Padding(1).Render(strings.Join(rows, "\n"))
}

func (m Model) settingsSelectedDomain(p palette) string {
	section := settingsSections[m.settingsCursor]
	return lipgloss.NewStyle().Foreground(p.cyan).Bold(true).Render("▌ " + section.label + " / " + section.name)
}

func (m Model) settingsControl(p palette, width int) string {
	// 2 columns of border (left+right) + 4 of Padding(1, 2)'s horizontal
	// component (2 each side) = 6 -- the room the box itself consumes
	// before any of its own content. wrapText below wraps every
	// possibly-long plain-text line to this budget explicitly, rather than
	// leaning on the final Width(width) box to wrap its own multi-line
	// content correctly (see wrapText's own doc comment for the confirmed
	// lipgloss bug that made it not safe to rely on for this).
	innerWidth := max(1, width-6)
	section := settingsSections[m.settingsCursor]
	rows := []string{
		lipgloss.NewStyle().Foreground(p.cyan).Bold(true).Render(section.label + " / " + section.name),
		lipgloss.NewStyle().Foreground(p.muted).Render(wrapText(section.summary, innerWidth)), "",
	}
	settings := model.EffectiveProjectSettings(m.state.ProjectSettings)
	switch m.settingsCursor {
	case 0:
		rows = append(rows,
			settingLine("Default task lease", settings.DefaultLease),
			settingLine("Stale-owner grace", settings.StaleGrace),
			settingLine("Active history window", settings.ActiveRetention),
			settingLine("Summary boundary", fmt.Sprintf("%d chars", settings.SummaryLimit)),
			settingLine("Artifact boundary", fmt.Sprintf("%d MiB", settings.ArtifactLimitBytes/(1024*1024))),
			settingLine("Review gate", enabledLabel(settings.RequireReview)),
			"", lipgloss.NewStyle().Foreground(p.amber).Render("[e] edit and sign project policy"),
		)
	case 1:
		activeAgents, policies := 0, len(m.state.InvocationPolicies)
		for _, agent := range m.state.Agents {
			if agent.Status == "ACTIVE" {
				activeAgents++
			}
		}
		rows = append(rows,
			settingLine("Active principals", strconv.Itoa(activeAgents)),
			settingLine("Invocation policies", strconv.Itoa(policies)),
			settingLine("Your authority", m.actorAuthority()),
			"", wrapText("Manage activation, suspension, roles, scopes, keys, and per-agent invocation trust.", innerWidth),
			"", lipgloss.NewStyle().Foreground(p.amber).Render("[e] open agent administration"),
		)
	case 2:
		online := 0
		for _, runtime := range m.state.AgentRuntimes {
			if runtime.Status == "ONLINE" {
				online++
			}
		}
		rows = append(rows,
			settingLine("Registered", strconv.Itoa(len(m.state.AgentRuntimes))),
			settingLine("Online", strconv.Itoa(online)),
			wrapText("Connectors use configuration references; secret values are never entered in this screen.", innerWidth),
			"", lipgloss.NewStyle().Foreground(p.amber).Render("[e] open runtime administration"),
		)
	case 3:
		rows = append(rows,
			settingLine("Consistency", empty(m.state.Integrity.Consistency, "UNKNOWN")),
			settingLine("Connectivity", empty(m.state.Integrity.Connectivity, "LOCAL")),
			settingLine("Server sequence", strconv.FormatUint(m.state.Integrity.ServerSequence, 10)),
			settingLine("Cache sequence", strconv.FormatUint(m.state.Integrity.CacheSequence, 10)),
			settingLine("Chain verified", enabledLabel(m.state.Integrity.Verified)),
			"", wrapText("Internal runtime storage is hidden by default. Use Audit & health for diagnostics.", innerWidth),
			"", lipgloss.NewStyle().Foreground(p.red).Bold(true).Render("[e] DANGER ZONE -- permanently delete this project"),
		)
	case 4:
		rows = append(rows,
			settingLine("Keys set", strconv.Itoa(len(m.state.Env))),
			wrapText("Plain-text, project-scoped configuration values -- never store secrets here.", innerWidth),
			"", lipgloss.NewStyle().Foreground(p.amber).Render("[e] open environment administration"),
		)
	case 5:
		theme := "automatic"
		if m.highContrast {
			theme = "high contrast"
		}
		rows = append(rows,
			settingLine("Theme", theme),
			settingLine("Scope", "this user"),
			wrapText("Interface choices do not create events or affect other collaborators.", innerWidth),
			"", lipgloss.NewStyle().Foreground(p.amber).Render("[e] toggle theme"),
		)
	}

	role := m.actorAuthority()
	shared := m.settingsCursor != 5
	scope, boundary := "LOCAL PREFERENCE", "Saved for this user."
	color := p.cyan
	if shared {
		scope, boundary = "SIGNED GOVERNANCE", "Validated by authority & visible project-wide."
		color = p.amber
	}
	rows = append(rows, "",
		lipgloss.NewStyle().Foreground(color).Bold(true).Render("◈ "+scope)+"  "+
			lipgloss.NewStyle().Foreground(p.muted).Render(fmt.Sprintf("(Actor: %s · Role: %s)", m.actor, role)),
		lipgloss.NewStyle().Foreground(p.muted).Render(wrapText(boundary+" Internal data directory hidden by default.", innerWidth)),
	)

	return lipgloss.NewStyle().Width(width).Border(lipgloss.NormalBorder()).BorderForeground(p.cyan).Padding(1, 2).Render(strings.Join(rows, "\n"))
}

func (m Model) actorAuthority() string {
	agent, ok := m.state.Agents[m.actor]
	if !ok {
		return "unregistered"
	}
	return strings.ToLower(string(agent.Role))
}

func settingLine(label, value string) string {
	return fmt.Sprintf("%-22s %s", label, value)
}

func enabledLabel(value bool) string {
	if value {
		return "enabled"
	}
	return "disabled"
}
