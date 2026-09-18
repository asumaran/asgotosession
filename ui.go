package main

// The bubbletea model: a filter input on top, a two-column body (sessions
// left, conversation preview right) and a help footer. Modeled on gotonotes
// and gotopr: the input is focused before the program starts, every printable
// key filters, and the chosen session is resumed AFTER the TUI exits
// (quitting is what closes the popup).

import (
	"strings"
	"time"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textinput"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

func truncate(s string, width int) string {
	return ansi.Truncate(s, width, "…")
}

// padRight truncates s to width and pads it with spaces up to width.
func padRight(s string, width int) string {
	s = truncate(s, width)
	if n := width - ansi.StringWidth(s); n > 0 {
		s += strings.Repeat(" ", n)
	}
	return s
}

// padLeft right-aligns s in width.
func padLeft(s string, width int) string {
	s = truncate(s, width)
	if n := width - ansi.StringWidth(s); n > 0 {
		s = strings.Repeat(" ", n) + s
	}
	return s
}

// ---- styles ----

var (
	stPrompt = lipgloss.NewStyle().Foreground(lipgloss.Color("13")).Bold(true)
	stDev    = lipgloss.NewStyle().Foreground(lipgloss.Color("208")).Bold(true)
	stSel    = lipgloss.NewStyle().Background(lipgloss.Color("8")).Bold(true)
	stMatch  = lipgloss.NewStyle().Foreground(lipgloss.Color("11"))
	stHeader = lipgloss.NewStyle().Foreground(lipgloss.Color("6")).Bold(true)
	stDim    = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	stTitle  = lipgloss.NewStyle().Bold(true)
	stError  = lipgloss.NewStyle().Foreground(lipgloss.Color("9")).Bold(true)
	stLive   = lipgloss.NewStyle().Foreground(lipgloss.Color("10"))
	stUser   = lipgloss.NewStyle().Foreground(lipgloss.Color("6")).Bold(true)
)

// ---- key bindings ----

type keyMap struct {
	Up       key.Binding
	Down     key.Binding
	Open     key.Binding
	Here     key.Binding
	Toggle   key.Binding
	Quit     key.Binding
	PrevUp   key.Binding
	PrevDown key.Binding
	Filter   key.Binding
}

func (k keyMap) ShortHelp() []key.Binding {
	return []key.Binding{k.Filter, k.Open, k.Here, k.Toggle, k.PrevDown, k.Quit}
}
func (k keyMap) FullHelp() [][]key.Binding { return [][]key.Binding{k.ShortHelp()} }

func defaultKeys() keyMap {
	return keyMap{
		Up:       key.NewBinding(key.WithKeys("up", "ctrl+p"), key.WithHelp("↑/^p", "up")),
		Down:     key.NewBinding(key.WithKeys("down", "ctrl+n"), key.WithHelp("↓/^n", "down")),
		Open:     key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "resume")),
		Here:     key.NewBinding(key.WithKeys("tab"), key.WithHelp("tab", "this dir")),
		Toggle:   key.NewBinding(key.WithKeys("ctrl+a"), key.WithHelp("^a", "missing dirs")),
		Quit:     key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc/q", "quit")),
		PrevUp:   key.NewBinding(key.WithKeys("shift+up", "pgup"), key.WithHelp("⇧↑", "")),
		PrevDown: key.NewBinding(key.WithKeys("shift+down", "pgdown"), key.WithHelp("⇧↓", "scroll preview")),
		// Help-only entry: a binding without keys is disabled and the help
		// bubble would skip it. Nothing ever matches against it.
		Filter: key.NewBinding(key.WithKeys("type"), key.WithHelp("type", "filter")),
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
		ti:       newFilterInput(),
		listVP:   viewport.New(viewport.WithWidth(50), viewport.WithHeight(20)),
		prevVP:   viewport.New(viewport.WithWidth(40), viewport.WithHeight(20)),
		help:     help.New(),
		keys:     defaultKeys(),
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

// newFilterInput builds the focused filter textinput with the gotosession
// prompt. The prompt string already carries its colors, so the prompt style
// is left empty.
func newFilterInput() textinput.Model {
	ti := textinput.New()
	ti.Prompt = promptText()
	st := ti.Styles()
	st.Focused.Prompt = lipgloss.NewStyle()
	st.Blurred.Prompt = lipgloss.NewStyle()
	ti.SetStyles(st)
	ti.Focus()
	return ti
}

// promptText builds the textinput prompt, with an orange "(dev)" marker on
// non-release builds.
func promptText() string {
	if strings.HasPrefix(version, "v") {
		return stPrompt.Render("gotosession ❯ ")
	}
	return stPrompt.Render("gotosession (") + stDev.Render("dev") + stPrompt.Render(") ❯ ")
}

func (m *model) current() *session {
	if m.cursor >= 0 && m.cursor < len(m.rows) {
		return m.rows[m.cursor].s
	}
	return nil
}

// ---- layout ----

func (m *model) listW() int {
	w := (m.width - 3) / 2
	if w < 20 {
		w = 20
	}
	return w
}

func (m *model) prevW() int {
	w := m.width - m.listW() - 3
	if w < 10 {
		w = 10
	}
	return w
}

// listTop is the screen row where the list starts: below the input.
const listTop = 1

func (m *model) bodyH() int {
	h := m.height - listTop - 1 // footer
	if h < 1 {
		h = 1
	}
	return h
}

func (m *model) resize() {
	m.listVP.SetWidth(m.listW())
	m.listVP.SetHeight(m.bodyH())
	m.prevVP.SetWidth(m.prevW())
	m.prevVP.SetHeight(m.bodyH())
	m.help.SetWidth(m.width)
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
	if q != "" {
		m.cursor = bestIndex(len(m.rows), func(i int) int { return m.rows[i].score })
	}
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
// age. The selected row is padded to the full width before styling so its
// background spans the whole column.
func (m *model) sessionLine(r sessionRow, selected bool, width int) string {
	pathW := (width - 2) * 2 / 5
	if pathW < 12 {
		pathW = 12
	}
	titleW := width - 2 - 1 - pathW - 1 - ageColW
	if titleW < 8 {
		titleW = 8
	}
	dir := tildePath(r.s.cwd, m.home)
	age := padLeft(compactAge(r.s.last, m.now), ageColW)
	mark := " "
	if r.s.pane != "" {
		mark = "●"
	}
	if selected {
		line := "▌" + mark + padRight(r.s.label(), titleW) + " " +
			padRight(pathCells(dir, nil, pathW, false), pathW) + " " + age
		return stSel.Render(padRight(line, width))
	}
	base := stTitle
	switch {
	case r.s.missing:
		base = stDim
	case r.s.title == "":
		base = lipgloss.NewStyle() // a prompt standing in for a title: not bold
	}
	title := padRight(highlight(r.s.label(), r.titleIdx, base), titleW)
	path := padRight(pathCells(dir, r.pathIdx, pathW, true), pathW)
	return truncate(" "+stLive.Render(mark)+title+" "+path+" "+stDim.Render(age), width)
}

// pathCells fits a path into width. A path that does not fit loses its head,
// not its tail, so the worktree name is always visible. The path is dimmed
// and the matched bytes highlighted when styled is set.
func pathCells(path string, idx []int, width int, styled bool) string {
	type cell struct {
		r   rune
		off int
	}
	cells := make([]cell, 0, len(path))
	for off, r := range path {
		cells = append(cells, cell{r, off})
	}
	cut := false
	if len(cells) > width && width > 1 {
		cells = cells[len(cells)-(width-1):]
		cut = true
	}
	matched := make(map[int]bool, len(idx))
	for _, i := range idx {
		matched[i] = true
	}
	var b, run strings.Builder
	flush := func() {
		if run.Len() == 0 {
			return
		}
		if styled {
			b.WriteString(stDim.Render(run.String()))
		} else {
			b.WriteString(run.String())
		}
		run.Reset()
	}
	if cut {
		run.WriteString("…")
	}
	for _, c := range cells {
		if styled && matched[c.off] {
			flush()
			b.WriteString(stMatch.Render(string(c.r)))
			continue
		}
		run.WriteRune(c.r)
	}
	flush()
	return b.String()
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
		// The wheel always scrolls the preview, wherever the pointer is; the
		// list is driven by the keys and by clicking a row (see gotopr).
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
	case key.Matches(msg, m.keys.Up):
		m.setCursor(m.cursor - 1)
		m.renderList()
		return m, m.updatePreview()
	case key.Matches(msg, m.keys.Down):
		m.setCursor(m.cursor + 1)
		m.renderList()
		return m, m.updatePreview()
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

// handleClick moves the cursor to the row under a left click on the list. It
// never resumes anything: that stays on enter.
func (m model) handleClick(msg tea.MouseClickMsg) (tea.Model, tea.Cmd) {
	if msg.Button != tea.MouseLeft || msg.X >= m.listW()+2 || msg.Y < listTop {
		return m, nil
	}
	i := msg.Y - listTop + m.listVP.YOffset()
	if i < 0 || i >= len(m.rows) || i == m.cursor {
		return m, nil
	}
	m.setCursor(i)
	m.renderList()
	return m, m.updatePreview()
}

func (m model) View() tea.View {
	sep := stDim.Render(strings.TrimRight(strings.Repeat("│\n", m.bodyH()), "\n"))
	body := lipgloss.JoinHorizontal(lipgloss.Top, m.leftColumn(), " ", sep, " ", m.prevVP.View())
	v := tea.NewView(m.ti.View() + "\n" + body + "\n" + m.footer())
	v.AltScreen = true
	v.MouseMode = tea.MouseModeCellMotion
	return v
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
	return lipgloss.NewStyle().Width(m.listW()).Height(m.bodyH()).
		Render(stDim.Render(truncate(msg, m.listW())))
}

func (m model) footer() string {
	f := m.help.View(m.keys)
	if m.notice != "" {
		return f + "  " + stError.Render(truncate(m.notice, m.width/2))
	}
	scope := countLabel(len(m.rows))
	if m.here {
		scope += " in " + tildePath(m.hereDir, m.home)
	}
	return f + "  " + stDim.Render(truncate(scope, m.width/3))
}
