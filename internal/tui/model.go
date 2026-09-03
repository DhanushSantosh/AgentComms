package tui

import (
	"context"
	"fmt"
	"io"
	"time"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"github.com/DhanushSantosh/AgentComms/internal/buildinfo"
	"github.com/DhanushSantosh/AgentComms/internal/controlplane"
	"github.com/DhanushSantosh/AgentComms/internal/doctor"
	"github.com/DhanushSantosh/AgentComms/internal/identity"
	"github.com/DhanushSantosh/AgentComms/internal/model"
	"github.com/DhanushSantosh/AgentComms/internal/projectlifecycle"
	"github.com/DhanushSantosh/AgentComms/internal/service"
	"github.com/fsnotify/fsnotify"
)

var views = []string{
	"Overview", "My work", "Tasks", "Inbox", "Agents", "Approvals", "Invocations",
	"Runtimes", "Project settings", "Documents", "Contracts & decisions", "Artifacts", "Drafts",
	"Blockers", "Audit & health", "Activity", "Archive search", "Environment",
}

type navigationHub struct {
	Name  string
	Views []string
}

var navigationHubs = []navigationHub{
	{Name: "Command", Views: []string{"Overview", "My work", "Blockers", "Approvals"}},
	{Name: "Work", Views: []string{"Tasks", "Documents", "Contracts & decisions", "Artifacts", "Drafts", "Archive search"}},
	{Name: "Team", Views: []string{"Agents", "Runtimes"}},
	{Name: "Relay", Views: []string{"Inbox", "Invocations", "Activity"}},
	{Name: "Project", Views: []string{"Project settings", "Environment", "Audit & health"}},
}

type Model struct {
	svc            *service.Service
	state          model.State
	actor          string
	projectID      string
	width, height  int
	view, cursor   int
	palette        bool
	query, notice  string
	err            error
	highContrast   bool
	form           string
	inputs         []textinput.Model
	formFocus      int
	formTaskID     string
	formSpec       *ActionForm
	rowFocus       bool
	taskList       RowList
	messageList    RowList
	approvalList   RowList
	agentList      RowList
	invocationList RowList
	runtimeList    RowList
	documentList   RowList
	decisionList   RowList
	artifactList   RowList
	envList        RowList
	drafts         []controlplane.Draft
	settingsFocus  bool
	settingsCursor int
	confirm        *confirmState
	watcher        *fsnotify.Watcher
	lifecycle      projectlifecycle.Plan
	findings       []doctor.Finding
	inspecting     bool
	toastMsg       string
	toastExpiresAt time.Time
	lastSeq        uint64
	// staleReads counts consecutive refreshSilent failures in a row (reset
	// to 0 on any successful read) -- refreshSilent swallows read errors so
	// a just-shown action result or notice never gets stomped by a routine
	// background tick, but that used to mean a daemon that went unreachable
	// left the TUI showing the same last-known-good data indefinitely, with
	// no signal to the human operator that it might now be stale. See
	// staleReadThreshold's own comment for why the indicator only appears
	// after several failures, not the first one.
	staleReads   int
	scrollOffset int
	// lastClickX/Y/At track the previous left click's exact cell and time,
	// purely to recognize a second click on that same cell within
	// doubleClickWindow as a double-click (see isDoubleClick) -- bubbletea
	// reports raw clicks with no click-count of its own.
	lastClickX, lastClickY int
	lastClickAt            time.Time
	ptySnapshots           map[string]string
	// exitNotice, when non-empty, is printed to Run's out writer once the
	// bubbletea program actually exits -- for a final message that needs to
	// survive past the alt-screen/raw-mode teardown, unlike m.notice (drawn
	// inside the still-running View). Its only current use is Danger Zone
	// project deletion (RFC 0020): once DeleteProject succeeds, the Store
	// this Model was built against no longer exists, so there is no view
	// left to return to -- Dispatch sets this and quits, exactly like "q".
	exitNotice string
}

// doubleClickWindow bounds how long after a first click a second click on
// the same cell still counts as a double-click, rather than two unrelated
// single clicks.
const doubleClickWindow = 500 * time.Millisecond

// isDoubleClick reports whether (x, y) at now forms a double-click with
// the immediately preceding one, and records this click as the new
// "previous" one either way -- callers observe the double-click exactly
// once, on the second click, never retroactively on the first.
func (m *Model) isDoubleClick(x, y int, now time.Time) bool {
	double := x == m.lastClickX && y == m.lastClickY && !m.lastClickAt.IsZero() && now.Sub(m.lastClickAt) <= doubleClickWindow
	m.lastClickX, m.lastClickY, m.lastClickAt = x, y, now
	return double
}

func New(s *service.Service, actor string) (Model, error) {
	st, e := s.State()
	projectID := "local project"
	owner := ""
	if config, err := s.Store.Config(); err == nil {
		if config.ProjectID != "" {
			projectID = config.ProjectID
		}
		owner = config.Owner
	}
	hc := false
	if uc, err := identity.LoadUserConfig(); err == nil && uc.Theme == "high-contrast" {
		hc = true
	}
	lifecycle, _, _ := projectlifecycle.Inspect(s.Store.Root, buildinfo.Version, buildinfo.ResolvedBuildID())
	// findings deliberately NOT computed here: doctor.Findings dials every
	// ONLINE interactive runtime's local PTY socket, which against a real
	// busy session can take seconds -- fine as a one-shot cost paid when
	// Audit & health is actually opened (focusCurrentView), not acceptable
	// as a blocking cost on every TUI launch. See refreshSilent's comment
	// for the matching reason it's excluded from the background tick too.
	drafts, _ := s.Drafts(50)
	return Model{
		svc: s, state: st, actor: actor, projectID: projectID, width: 100, height: 30, highContrast: hc,
		taskList: newRowList(taskRowSource{}), messageList: newRowList(messageRowSource{owner: owner}),
		approvalList: newRowList(approvalRowSource{}), agentList: newRowList(agentRowSource{}),
		invocationList: newRowList(invocationRowSource{}), runtimeList: newRowList(runtimeRowSource{root: s.Store.Root}),
		documentList: newRowList(documentRowSource{}), decisionList: newRowList(decisionRowSource{}),
		artifactList: newRowList(artifactRowSource{}), envList: newRowList(envRowSource{}),
		lifecycle: lifecycle, drafts: drafts, lastSeq: st.Integrity.ServerSequence,
	}, e
}
func (m Model) Init() tea.Cmd {
	if m.watcher != nil {
		return tea.Batch(tea.RequestBackgroundColor, watchEventsCmd(m.watcher))
	}
	return tea.RequestBackgroundColor
}
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	// Handled before any mode dispatch below: a resize while a form,
	// confirm dialog, row list, or settings pane has focus used to never
	// reach the tea.WindowSizeMsg case further down at all (form/confirm/
	// rowFocus/settingsFocus each return out of one of their own
	// mode-specific update functions first), leaving m.width/m.height
	// stale -- silently wrong layout, and stale inputs to the row-list
	// click math in mouse.go -- until something returned to plain
	// navigation mode.
	if resize, ok := msg.(tea.WindowSizeMsg); ok {
		m.width = resize.Width
		m.height = resize.Height
		m.syncActiveRowListDimensions()
		return m, nil
	}
	if snapMsg, ok := msg.(ptySnapshotMsg); ok {
		if snapMsg.err == nil {
			if m.ptySnapshots == nil {
				m.ptySnapshots = make(map[string]string)
			}
			m.ptySnapshots[snapMsg.runtimeID] = snapMsg.snapshot
		}
		return m, nil
	}
	if _, ok := msg.(fsEventMsg); ok {
		m.refreshSilent()
		cmd := m.fetchSelectedRuntimePTYSnapshotCmd()
		if m.watcher != nil {
			return m, tea.Batch(watchEventsCmd(m.watcher), cmd)
		}
		return m, cmd
	}
	// Also handled before any mode dispatch, for the same reason as the
	// resize above: a sidebar click has to work regardless of what's
	// currently focused, not just from plain navigation mode. It used to
	// live in the switch below, reachable only when none of
	// form/confirm/rowFocus/settingsFocus were set -- fine for the very
	// first click, but that click's own openView+focusCurrentView sets
	// rowFocus, so it was the LAST sidebar click that mode session ever
	// saw: every click after it got routed to updateRowList instead, which
	// has no idea what a sidebar hub is and just swallowed it. Clicking a
	// different section is an unambiguous "go there now" that should win
	// over whatever mode the previous click (or keypress) left behind, the
	// same way it would in any mouse-native app.
	//
	// Checked before this, unconditionally: the command palette. It used
	// to be handled far below, nested inside the plain-navigation switch --
	// reachable only once form/confirm/rowFocus/settingsFocus were all
	// false. Opening the palette from inside a focused row list (rowlist.go's
	// own "/" case) set m.palette=true but never cleared m.rowFocus, so
	// every subsequent keystroke kept routing straight back to
	// updateRowList instead: typed characters silently triggered row
	// actions -- suspend, revoke, delete, "n" opening an unrelated form --
	// while the palette sat on screen showing an empty, frozen query box.
	// Checking m.palette first, here, means opening it from any mode
	// always wins for input immediately after, exactly like the sidebar
	// and hub-tab clicks below already do -- and since nothing needs to
	// touch m.rowFocus/m.settingsFocus to get that, closing the palette
	// (Esc, or a successful Enter) falls straight back through to whatever
	// they already were, landing exactly where the palette was opened
	// from with no separate state to save and restore.
	if m.palette {
		return m.updatePalette(msg)
	}
	if click, ok := msg.(tea.MouseClickMsg); ok {
		mouse := click.Mouse()
		if mouse.Button == tea.MouseLeft {
			p := colors(m.highContrast)
			if hub, ok := m.sidebarHubAt(p, mouse.X, mouse.Y); ok {
				m.form, m.inputs, m.formSpec, m.confirm, m.rowFocus, m.settingsFocus = "", nil, nil, nil, false, false
				m.openView(navigationHubs[hub].Views[0])
				m.focusCurrentView()
				return m, nil
			}
			// Any click inside the sidebar area that didn't land on a hub
			// label is a no-op -- never let it fall through to table row
			// selection or any other content-pane handler.
			if mouse.X < m.sidebarWidth() {
				return m, nil
			}
			// Same "always wins, before any mode dispatch" treatment as the
			// sidebar above, and for the identical reason: switching tabs
			// within the current hub has to work no matter what's currently
			// focused, not just from plain navigation mode.
			if view, ok := m.hubTabAt(p, mouse.X, mouse.Y); ok {
				m.form, m.inputs, m.formSpec, m.confirm, m.rowFocus, m.settingsFocus = "", nil, nil, nil, false, false
				m.openView(view)
				m.focusCurrentView()
				return m, nil
			}
		}
	}
	if m.form != "" {
		return m.updateForm(msg)
	}
	if m.confirm != nil {
		return m.updateConfirm(msg)
	}
	if m.rowFocus {
		return m.updateRowList(msg)
	}
	if m.settingsFocus {
		return m.updateSettings(msg)
	}
	if wheel, ok := msg.(tea.MouseWheelMsg); ok {
		switch wheel.Button {
		case tea.MouseWheelUp:
			m.scrollOffset = max(0, m.scrollOffset-3)
		case tea.MouseWheelDown:
			m.scrollOffset += 3
		}
		return m, nil
	}
	switch v := msg.(type) {
	case tea.KeyPressMsg:
		k := v.String()
		switch k {
		case "q", "ctrl+c":
			return m, tea.Quit
		case "pgdown", "ctrl+d":
			m.scrollOffset += 8
		case "pgup", "ctrl+u":
			m.scrollOffset = max(0, m.scrollOffset-8)
		case "up", "k":
			m.moveHub(-1)
		case "down", "j":
			m.moveHub(1)
		case "left":
			m.moveHubView(-1)
		case "right":
			m.moveHubView(1)
		case "[":
			m.moveHubView(-1)
		case "]":
			m.moveHubView(1)
		case "enter":
			m.view = m.cursor
			m.focusCurrentView()
		case "/", "ctrl+p":
			m.palette = true
		case "o":
			m.openView("Overview")
		case "g":
			m.openView("Agents")
		case "i":
			m.openView("Invocations")
		case "r":
			m.refresh()
		case "?":
			m.notice = "↑/↓ navigate · → open · ← back · / commands · a switch actor · r refresh · q quit · agent-comms agent-instructions for the full guide"
		case "h":
			m.highContrast = !m.highContrast
			theme := "auto"
			if m.highContrast {
				theme = "high-contrast"
			}
			if uc, err := identity.LoadUserConfig(); err == nil {
				uc.Theme = theme
				_ = identity.SaveUserConfig(uc)
			}
		case "n":
			return m.openCreateForm()
		case "a":
			return m.openActorSwitchForm()
		}
	}
	return m, nil
}

func (m *Model) openView(name string) {
	m.scrollOffset = 0
	for index, viewName := range views {
		if viewName == name {
			m.view = index
			m.cursor = index
			m.notice = ""
			m.refreshView(name)
			return
		}
	}
}

func (m *Model) activeHubIndex() int {
	current := views[m.view]
	for hubIndex, hub := range navigationHubs {
		for _, viewName := range hub.Views {
			if viewName == current {
				return hubIndex
			}
		}
	}
	return 0
}

func (m *Model) moveHub(delta int) {
	next := max(0, min(len(navigationHubs)-1, m.activeHubIndex()+delta))
	m.openView(navigationHubs[next].Views[0])
	m.notice = ""
}

func (m *Model) moveHubView(delta int) {
	hub := navigationHubs[m.activeHubIndex()]
	current := views[m.view]
	position := 0
	for index, viewName := range hub.Views {
		if viewName == current {
			position = index
			break
		}
	}
	position = (position + delta + len(hub.Views)) % len(hub.Views)
	m.openView(hub.Views[position])
	m.notice = ""
}

// refreshView reloads whatever data the named view displays. Called both
// from openView (so switching tabs by arrow key, letter shortcut, or the
// palette shows live content immediately, not "No rows here yet." until
// Enter is next pressed) and from focusCurrentView (so re-entering a view
// you're already on with Enter still picks up any change since the last
// refresh).
func (m *Model) refreshView(name string) {
	switch name {
	case "Tasks", "My work":
		m.taskList.SetMineFilter(name == "My work", m.state, m.actor)
	case "Inbox":
		m.messageList.Refresh(m.state, m.actor)
	case "Approvals":
		m.approvalList.Refresh(m.state, m.actor)
	case "Agents":
		m.agentList.Refresh(m.state, m.actor)
	case "Invocations":
		m.invocationList.Refresh(m.state, m.actor)
	case "Runtimes":
		m.runtimeList.Refresh(m.state, m.actor)
	case "Documents":
		m.documentList.Refresh(m.state, m.actor)
	case "Contracts & decisions":
		m.decisionList.Refresh(m.state, m.actor)
	case "Artifacts":
		m.artifactList.Refresh(m.state, m.actor)
	case "Environment":
		m.envList.Refresh(m.state, m.actor)
	case "Drafts":
		m.refreshDrafts()
	case "Audit & health":
		m.refreshFindings()
	}
}

// focusCurrentView enters interactive per-row mode (up/down selects a row,
// contextual action keys apply to it) for the view Enter was just pressed
// on. Content itself is already live by this point via openView's own
// refreshView call; this only adds selection/action capability.
func (m *Model) focusCurrentView() {
	name := views[m.view]
	m.refreshView(name)
	switch name {
	case "Tasks", "My work", "Inbox", "Approvals", "Agents", "Invocations",
		"Runtimes", "Documents", "Contracts & decisions", "Artifacts", "Environment":
		m.rowFocus = true
	case "Project settings":
		m.settingsFocus = true
	}
}

// refresh re-reads state and reports that it did so via m.notice. Use this
// only where nothing more specific was already reported this same tick (the
// bare "r" refresh keybinding, essentially) -- every other call site that
// already set an informative m.notice (e.g. "Applied agent.activate to
// THOR") must call refreshState instead, or this overwrites it before it
// ever renders. Confirmed live as a real bug: the actor-switch form set
// m.notice = "Switched to " + candidate immediately followed by a refresh()
// call, so that confirmation was never actually visible -- only the generic
// "State refreshed at HH:MM:SS" that replaced it a line later.
func (m *Model) refresh() {
	m.refreshState()
	if m.err == nil {
		m.notice = "State refreshed at " + time.Now().Format("15:04:05")
	}
}

// refreshState re-reads state, lists, findings, and drafts without touching
// m.notice, so a caller's own just-set result notice survives. See refresh's
// doc comment for when to use which.
func (m *Model) refreshState() {
	m.state, m.err = m.svc.State()
	if m.err == nil {
		m.refreshLists()
		m.refreshFindings()
		m.refreshDrafts()
	}
}

// refreshSilent re-reads state without disturbing the current notice/error,
// used by the background file-watch tick so it never stomps a just-shown
// action result. Read errors are swallowed; the last-known-good state stays
// displayed until the next successful read. Deliberately does NOT call
// refreshFindings: doctor.Findings dials every ONLINE interactive runtime's
// local PTY socket to check it's alive, and against a real, busy session
// that round-trip can take seconds, not milliseconds -- fine as a one-shot
// cost (New, or an explicit 'r' refresh) but not something to repeat on
// every background tick a file-watch event fires. Findings still refresh
// on the next real refresh() call; the last-known-good findings stay
// displayed in between, the same tradeoff refreshDrafts already makes for
// the same reason.
// staleReadThreshold is how many consecutive refreshSilent failures in a
// row it takes before the TUI admits its data might be stale. Not the
// first failure: a background file-watch tick fires often enough that one
// transient blip (a daemon mid-restart, a momentary socket hiccup) would
// otherwise flash the indicator on and off constantly under perfectly
// normal operation. Several in a row is a real, sustained problem worth
// surfacing.
const staleReadThreshold = 3

func (m *Model) refreshSilent() {
	st, err := m.svc.State()
	if err != nil {
		m.staleReads++
		return
	}
	m.staleReads = 0
	if m.lastSeq > 0 && st.Integrity.ServerSequence > m.lastSeq {
		m.toastMsg = fmt.Sprintf("🔔 Event #%d committed in background", st.Integrity.ServerSequence)
		m.toastExpiresAt = time.Now().Add(4 * time.Second)
	}
	m.lastSeq = st.Integrity.ServerSequence
	m.state = st
	m.refreshLists()
}

// refreshFindings recomputes doctor's health findings using the exact same
// logic `agent-comms doctor` runs (internal/doctor.Findings) so Audit &
// health can never silently drift from the CLI's own picture of project
// health. Errors are swallowed the same way refreshSilent swallows state
// read errors -- the last-known-good findings stay displayed rather than
// disappearing.
func (m *Model) refreshFindings() {
	if findings, err := doctor.Findings(context.Background(), m.svc); err == nil {
		m.findings = findings
	}
}
func (m *Model) refreshLists() {
	m.taskList.Refresh(m.state, m.actor)
	m.messageList.Refresh(m.state, m.actor)
	m.approvalList.Refresh(m.state, m.actor)
	m.agentList.Refresh(m.state, m.actor)
	m.invocationList.Refresh(m.state, m.actor)
	m.runtimeList.Refresh(m.state, m.actor)
	m.documentList.Refresh(m.state, m.actor)
	m.decisionList.Refresh(m.state, m.actor)
	m.artifactList.Refresh(m.state, m.actor)
	m.envList.Refresh(m.state, m.actor)
}

func Run(s *service.Service, actor string, in io.Reader, out io.Writer, opts ...tea.ProgramOption) error {
	m, e := New(s, actor)
	if e != nil {
		return e
	}
	m.EnableFileWatch()
	if m.watcher != nil {
		defer m.watcher.Close()
	}
	progOpts := append([]tea.ProgramOption{tea.WithInput(in), tea.WithOutput(out)}, opts...)
	p := tea.NewProgram(m, progOpts...)
	final, e := p.Run()
	if e != nil {
		return e
	}
	if fm, ok := final.(Model); ok && fm.exitNotice != "" {
		fmt.Fprintln(out, fm.exitNotice)
	}
	return nil
}
func RenderForTest(s *service.Service, actor string, w, h int) (string, error) {
	m, e := New(s, actor)
	if e != nil {
		return "", e
	}
	m.width = w
	m.height = h
	return m.View().Content, nil
}

type ptySnapshotMsg struct {
	runtimeID string
	snapshot  string
	err       error
}

func (m Model) fetchSelectedRuntimePTYSnapshotCmd() tea.Cmd {
	if views[m.view] != "Runtimes" || m.svc == nil || m.svc.Store.Root == "" {
		return nil
	}
	id := m.runtimeList.SelectedID(m.state, m.actor)
	if id == "" {
		return nil
	}
	root := m.svc.Store.Root
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
		defer cancel()
		snapshot, err := ptySnapshot(ctx, root, id)
		return ptySnapshotMsg{runtimeID: id, snapshot: snapshot, err: err}
	}
}
