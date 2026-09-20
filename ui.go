package main

// The bubbletea model: one frame (see frame.go) holding the filter input, the
// sessions next to the conversation preview, and the help.
// Modeled on asgotonotes and asgotopr: the input is focused before the program
// starts, every printable key filters, and the chosen session is resumed
// AFTER the TUI exits (quitting is what closes the popup).

import (
	"strconv"
	"strings"
	"time"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textinput"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

// ---- styles ----

var (
	stHeader = lipgloss.NewStyle().Foreground(lipgloss.Color("6")).Bold(true)
	stDim    = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	stTitle  = lipgloss.NewStyle().Bold(true)
	stError  = lipgloss.NewStyle().Foreground(lipgloss.Color("9")).Bold(true)
	stLive   = lipgloss.NewStyle().Foreground(lipgloss.Color("10"))
	stScope  = lipgloss.NewStyle().Foreground(lipgloss.Color("11"))
	stCount  = lipgloss.NewStyle().Foreground(lipgloss.Color("3"))
	stUser   = lipgloss.NewStyle().Foreground(lipgloss.Color("6")).Bold(true)
)

// ---- key bindings ----

type keyMap struct {
	Nav      listNav
	Open     key.Binding
	Here     key.Binding
	Toggle   key.Binding
	Quit     key.Binding
	PrevUp   key.Binding
	PrevDown key.Binding
	Shrink   key.Binding
	Grow     key.Binding
	Filter   key.Binding
	Help     key.Binding
}

// ShortHelp is the folded help line: the tool's own actions, the help and the
// quit keys. Moving, scrolling and resizing are in the expanded help, so the
// line stays short enough for a narrow popup (a cut line loses the quit keys
// first).
func (k keyMap) ShortHelp() []key.Binding {
	return []key.Binding{k.Filter, k.Open, k.Here, k.Toggle, k.Help, k.Quit}
}

// FullHelp is what `?` expands the help into, one column per group: the
// filter and the preview, the list, the tool's actions, help and quit.
func (k keyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{k.Filter, k.PrevUp, k.Shrink},
		{k.Nav.Up, k.Nav.PageUp, k.Nav.Top},
		{k.Open, k.Here, k.Toggle},
		{k.Help, k.Quit},
	}
}

func defaultKeys() keyMap {
	return keyMap{
		Nav:      defaultListNav(),
		Open:     key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "resume")),
		Here:     key.NewBinding(key.WithKeys("tab"), key.WithHelp("tab", "this dir")),
		Toggle:   key.NewBinding(key.WithKeys("ctrl+a"), key.WithHelp("^a", "missing dirs")),
		Quit:     key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc/q", "quit")),
		PrevUp:   key.NewBinding(key.WithKeys("shift+up"), key.WithHelp("⇧↑/⇧↓", "scroll preview")),
		PrevDown: key.NewBinding(key.WithKeys("shift+down")),
		Shrink:   key.NewBinding(key.WithKeys("shift+left"), key.WithHelp("⇧←/⇧→", "resize the list")),
		Grow:     key.NewBinding(key.WithKeys("shift+right")),
		// Help-only entry: a binding without keys is disabled and the help
		// bubble would skip it. Nothing ever matches against it.
		Filter: key.NewBinding(key.WithKeys("type"), key.WithHelp("type", "filter")),
		Help:   helpKey,
	}
}

// ---- model ----

type options struct {
	all   bool   // also list sessions whose directory is gone
	here  bool   // start narrowed to hereDir
	dir   string // what "this dir" means: the directory the popup was opened from
	query string // initial filter
}

type model struct {
	// data
	sessions []*session // every resumable transcript, newest first
	loadErr  string     // the transcripts could not be listed
	all      bool
	here     bool
	hereDir  string
	home     string
	now      time.Time

	rows   []sessionRow
	cursor int

	// ui
	notice string // transient footer message, cleared by the next key
	ti     textinput.Model
	listVP viewport.Model
	prevVP viewport.Model
	help   help.Model
	keys   keyMap
	width  int
	height int
	split  int // the preview's share of the width, percent

	// preview render cache
	renders map[string]string
	prevKey string

	chosen *session // session to resume after quit (nil = none)
}

func newModel(sessions []*session, loadErr string, opts options) model {
	m := model{
		sessions: sessions,
		loadErr:  loadErr,
		all:      opts.all,
		here:     opts.here && opts.dir != "",
		hereDir:  opts.dir,
		home:     homeDir(),
		now:      time.Now(),
		ti:       newFilterInput("asgotosession", "Search by title, directory, branch…"),
		listVP:   viewport.New(viewport.WithWidth(50), viewport.WithHeight(20)),
		prevVP:   viewport.New(viewport.WithWidth(40), viewport.WithHeight(20)),
		help:     help.New(),
		keys:     defaultKeys(),
		split:    loadSplit(stateDir()),
		renders:  map[string]string{},
		width:    94,
		height:   24,
	}
	m.ti.SetValue(opts.query)
	m.ti.CursorEnd()
	m.syncHelp()
	m.applyFilter()
	m.resize()
	m.renderList()
	return m
}

func (m *model) current() *session {
	if m.cursor >= 0 && m.cursor < len(m.rows) {
		return m.rows[m.cursor].s
	}
	return nil
}

// ---- layout ----

// innerW is the width inside the frame's sides.
func (m *model) innerW() int { return max(20, m.width-2) }

// detailsW is the preview's share of the main section, including the cell of
// padding on each side; prevW is the text width inside it.
func (m *model) detailsW() int { _, w := splitWidths(m.innerW(), m.split); return w }
func (m *model) prevW() int    { return max(10, m.detailsW()-2) }

// listW is what the divider leaves for the list.
func (m *model) listW() int { w, _ := splitWidths(m.innerW(), m.split); return w }

// bodyH is the height of the main section: everything but the frame's own
// lines and the help, which takes more of them while `?` has it expanded.
func (m *model) bodyH() int { return max(1, m.height-frameRows(false)-m.footH()) }

// footH is the height of the foot: a message takes one line, the help more
// while `?` has it expanded; the main section keeps at least minBodyH.
func (m *model) footH() int {
	if m.footMsg() != "" {
		return 1
	}
	return helpHeight(m.help, m.keys, m.height-frameRows(false)-minBodyH)
}

const minBodyH = 4

func (m *model) toggleHelp() tea.Cmd {
	m.help.ShowAll = !m.help.ShowAll
	m.resize()
	m.renderList()
	return m.updatePreview()
}

func (m *model) resize() {
	m.listVP.SetWidth(m.listW())
	m.listVP.SetHeight(m.bodyH())
	m.prevVP.SetWidth(m.prevW())
	m.prevVP.SetHeight(m.bodyH())
	m.help.SetWidth(max(0, m.width-4))
	sizeInput(&m.ti, m.width-4)
}

// resizeList moves the divider between the list and the preview by one step.
func (m *model) resizeList(grow bool) tea.Cmd {
	m.split = stepSplit(m.split, grow)
	saveSplit(stateDir(), m.split)
	m.resize()
	m.renderList()
	return m.updatePreview()
}

// ---- filtering ----

func (m *model) visible() []*session {
	here := ""
	if m.here {
		here = m.hereDir
	}
	return visibleSessions(m.sessions, m.all, here)
}

func (m *model) applyFilter() {
	q := m.ti.Value()
	m.rows = filterSessions(m.visible(), q, m.home)
	m.cursor = 0
	if len(m.rows) == 0 {
		m.cursor = -1
	}
}

func (m *model) keepCursorOn(id string) {
	for i, r := range m.rows {
		if r.s.id == id {
			m.cursor = i
			return
		}
	}
}

// refilter re-applies the query after a keystroke or a mode toggle. An empty
// query means nothing is being searched for, so the cursor stays on the row
// it was on instead of jumping to the top.
func (m *model) refilter() {
	id := ""
	if s := m.current(); s != nil {
		id = s.id
	}
	m.applyFilter()
	if m.ti.Value() == "" {
		m.keepCursorOn(id)
	}
}

// syncHelp makes each toggle describe what pressing it would do next.
func (m *model) syncHelp() {
	if m.all {
		m.keys.Toggle.SetHelp("^a", "hide missing")
	} else {
		m.keys.Toggle.SetHelp("^a", "missing dirs")
	}
	if m.here {
		m.keys.Here.SetHelp("tab", "everywhere")
	} else {
		m.keys.Here.SetHelp("tab", "this dir")
	}
	m.keys.Here.SetEnabled(m.hereDir != "")
}

// ---- list rendering ----

const ageColW = 4

func (m *model) renderList() {
	w := m.listW()
	var b strings.Builder
	for i, r := range m.rows {
		if i > 0 {
			b.WriteString("\n")
		}
		b.WriteString(m.sessionLine(r, i == m.cursor, w))
	}
	m.listVP.SetContent(b.String())
	m.ensureVisible()
}

// sessionLine renders one row in fixed columns: live mark, title, directory,
// age. A list too narrow for a readable title drops the directory, which the
// preview shows anyway. The selected row is padded to the full width before
// styling so its background spans the whole column.
func (m *model) sessionLine(r sessionRow, selected bool, width int) string {
	pathW := (width - 2) * 2 / 5
	if pathW < 12 {
		pathW = 12
	}
	titleW := width - 2 - 1 - pathW - 1 - ageColW
	if titleW < 24 {
		pathW, titleW = 0, max(8, titleW+pathW+1)
	}
	dir := tildePath(r.s.cwd, m.home)
	age := padLeft(compactAge(r.s.last, m.now), ageColW)
	mark := " "
	if r.s.pane != "" {
		mark = "●"
	}
	if selected {
		line := stSel.Render("▌"+mark) + highlight(padRight(r.s.label(), titleW), r.titleIdx, stSel) + stSel.Render(" ")
		if pathW > 0 {
			line += selPad(pathCells(dir, len(dir), r.pathIdx, pathW, true), pathW) + stSel.Render(" ")
		}
		return selPad(truncate(line+stSel.Render(age), width), width)
	}
	base := stTitle
	switch {
	case r.s.missing:
		base = stDim
	case r.s.title == "":
		base = lipgloss.NewStyle() // a prompt standing in for a title: not bold
	}
	line := " " + stLive.Render(mark) + padRight(highlight(r.s.label(), r.titleIdx, base), titleW) + " "
	if pathW > 0 {
		line += padRight(pathCells(dir, len(dir), r.pathIdx, pathW, false), pathW) + " "
	}
	return truncate(line+stDim.Render(age), width)
}

func (m *model) setCursor(i int) {
	if i < 0 || i >= len(m.rows) {
		return
	}
	m.cursor = i
}

func (m *model) ensureVisible() {
	h, c := m.listVP.Height(), m.cursor
	if h <= 0 || c < 0 {
		m.listVP.SetYOffset(0)
		return
	}
	if c < m.listVP.YOffset() {
		m.listVP.SetYOffset(c)
	} else if c >= m.listVP.YOffset()+h {
		m.listVP.SetYOffset(c - h + 1)
	}
}

// ---- preview ----

// updatePreview refreshes the right column with the session under the
// cursor, rendered off the update loop and cached.
func (m *model) updatePreview() tea.Cmd {
	s := m.current()
	if s == nil {
		m.prevKey = ""
		m.prevVP.SetContent("")
		return nil
	}
	key := previewKey(s.file, m.prevW())
	if key == m.prevKey {
		return nil
	}
	m.prevKey = key
	if c, ok := m.renders[key]; ok {
		m.showPreview(c)
		return nil
	}
	m.prevVP.SetContent(stDim.Render("reading…"))
	return renderPreviewCmd(*s, key, m.prevW())
}

// showPreview lands on the end of the conversation: where it was left is
// what tells one session from another.
func (m *model) showPreview(content string) {
	m.prevVP.SetContent(content)
	m.prevVP.GotoBottom()
}

// ---- resuming ----

// queueResume quits so the session can be resumed after the TUI is gone.
// With nowhere to resume it, it stays in the popup and says why: anything
// printed after the popup closes is lost.
func (m *model) queueResume(s *session) tea.Cmd {
	if s == nil {
		return nil
	}
	if !dirExists(s.cwd) {
		m.notice = "cannot resume: " + tildePath(s.cwd, m.home) + " no longer exists"
		return nil
	}
	m.chosen = s
	return tea.Quit
}

// ---- bubbletea ----

func (m model) Init() tea.Cmd {
	return tea.Batch(textinput.Blink, m.updatePreview())
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.resize()
		m.renderList()
		return m, m.updatePreview()

	case previewMsg:
		m.renders[msg.key] = msg.content
		if msg.key == m.prevKey {
			m.showPreview(msg.content)
		}
		return m, nil

	case tea.KeyPressMsg:
		return m.handleKey(msg)

	case tea.MouseWheelMsg:
		// Over the list the wheel moves the selection, as in asgitlog; anywhere
		// else it scrolls the preview.
		if m.overList(msg.X, msg.Y) {
			if k, ok := wheelKey(msg); ok {
				return m.handleKey(k)
			}
			return m, nil
		}
		m.prevVP, _ = m.prevVP.Update(msg)
		return m, nil

	case tea.MouseClickMsg:
		return m.handleClick(msg)

	default:
		var cmd tea.Cmd
		m.ti, cmd = m.ti.Update(msg)
		return m, cmd
	}
}

func (m model) handleKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	m.notice = ""
	switch {
	case msg.String() == "ctrl+c":
		return m, tea.Quit
	case foldsHelp(msg, m.help):
		return m, m.toggleHelp() // esc folds the help before it quits
	case isHelpKey(msg, m.ti.Value()):
		return m, m.toggleHelp()
	case msg.String() == "q" && m.ti.Value() == "":
		// q quits only while the filter is empty; otherwise it is text.
		return m, tea.Quit
	case key.Matches(msg, m.keys.Quit):
		return m, tea.Quit
	case key.Matches(msg, m.keys.Open):
		return m, m.queueResume(m.current())
	case key.Matches(msg, m.keys.Toggle):
		m.all = !m.all
		m.syncHelp()
		m.refilter()
		m.renderList()
		return m, m.updatePreview()
	case key.Matches(msg, m.keys.Here):
		m.here = !m.here
		m.syncHelp()
		m.refilter()
		m.renderList()
		return m, m.updatePreview()
	case m.keys.Nav.matches(msg):
		m.setCursor(m.keys.Nav.move(msg, m.cursor, len(m.rows), m.listVP.Height(), nil))
		m.renderList()
		return m, m.updatePreview()
	case key.Matches(msg, m.keys.Shrink):
		return m, m.resizeList(false)
	case key.Matches(msg, m.keys.Grow):
		return m, m.resizeList(true)
	case key.Matches(msg, m.keys.PrevUp):
		m.prevVP.ScrollUp(3)
		return m, nil
	case key.Matches(msg, m.keys.PrevDown):
		m.prevVP.ScrollDown(3)
		return m, nil
	}

	before := m.ti.Value()
	var cmd tea.Cmd
	m.ti, cmd = m.ti.Update(msg)
	if m.ti.Value() != before {
		m.refilter()
		m.renderList()
	}
	return m, tea.Batch(cmd, m.updatePreview())
}

// overList reports whether a screen cell is inside the list.
func (m *model) overList(x, y int) bool {
	return inList(x, y, listY(false), m.listW(), m.bodyH())
}

// handleClick moves the cursor to the row under a left click on the list. It
// never resumes anything: that stays on enter.
func (m model) handleClick(msg tea.MouseClickMsg) (tea.Model, tea.Cmd) {
	if msg.Button != tea.MouseLeft || !m.overList(msg.X, msg.Y) {
		return m, nil
	}
	i, ok := rowUnder(msg.Y, listY(false), m.listVP.YOffset(), len(m.rows))
	if !ok || i == m.cursor {
		return m, nil
	}
	m.setCursor(i)
	m.renderList()
	return m, m.updatePreview()
}

func (m model) View() tea.View {
	v := tea.NewView(m.render())
	v.AltScreen = true
	v.MouseMode = tea.MouseModeCellMotion
	return v
}

// render stacks the sections in one frame (see frame.go). There is no context
// line: what the list is narrowed to fits next to the counter.
func (m model) render() string {
	w := m.width
	out := frameHead(w, "", withDevMark(m.status()), m.ti.View())
	out = append(out, splitMain(m.listLines(), strings.Split(m.prevVP.View(), "\n"),
		m.listW(), m.detailsW(), m.counter(), scrollPos(&m.prevVP))...)
	for _, l := range m.footLines() {
		out = append(out, framed(w, l))
	}
	out = append(out, hline(w, "╰", "╯", "", ""))
	return strings.Join(out, "\n")
}

// counter is the matches/total count of the current mode, for the edge under
// the list.
func (m model) counter() string {
	return stCount.Render(strconv.Itoa(len(m.rows)) + "/" + strconv.Itoa(len(m.visible())))
}

// status is what the list is narrowed or widened to (the scope, as in
// asgitlog), for the edge over the input.
func (m model) status() string {
	var scope []string
	if m.here {
		// A long directory loses its head, not its tail, like the list's paths.
		scope = append(scope, "in "+pathTail(tildePath(m.hereDir, m.home), max(10, m.width/3)))
	}
	if m.all {
		scope = append(scope, "+missing dirs")
	}
	if len(scope) == 0 {
		return ""
	}
	return stScope.Render("[" + strings.Join(scope, ", ") + "]")
}

// listLines is the list as exactly bodyH lines of listW cells.
func (m model) listLines() []string {
	lines := strings.Split(m.leftColumn(), "\n")
	for len(lines) < m.bodyH() {
		lines = append(lines, "")
	}
	lines = lines[:m.bodyH()]
	for i, l := range lines {
		lines[i] = fit(l, m.listW())
	}
	return lines
}

// leftColumn is the list, or the reason there is nothing to list.
func (m model) leftColumn() string {
	if len(m.rows) > 0 {
		return m.listVP.View()
	}
	msg := "No matches"
	switch {
	case m.loadErr != "":
		msg = m.loadErr
	case m.ti.Value() != "":
	case m.here:
		msg = "No sessions in " + tildePath(m.hereDir, m.home) + " (tab: everywhere)"
	default:
		msg = "No sessions yet"
	}
	return stDim.Render(truncate(" "+msg, m.listW()))
}

// footer is the key help, or the notice while one is showing.
// footMsg is what takes the help's place while there is something to say.
func (m model) footMsg() string {
	if m.notice != "" {
		return stError.Render(truncate(m.notice, max(0, m.width-4)))
	}
	return ""
}

func (m model) footLines() []string {
	if msg := m.footMsg(); msg != "" {
		return []string{msg}
	}
	return helpLines(m.help, m.keys, m.width-4, m.footH())
}
