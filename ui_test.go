package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

func press(m model, keys ...tea.KeyPressMsg) model {
	for _, k := range keys {
		next, _ := m.Update(k)
		m = next.(model)
	}
	return m
}

func typed(s string) []tea.KeyPressMsg {
	var out []tea.KeyPressMsg
	for _, r := range s {
		out = append(out, tea.KeyPressMsg{Code: r, Text: string(r)})
	}
	return out
}

var (
	keyEnter = tea.KeyPressMsg{Code: tea.KeyEnter}
	keyDown  = tea.KeyPressMsg{Code: tea.KeyDown}
	keyTab   = tea.KeyPressMsg{Code: tea.KeyTab}
	keyCtrlA = tea.KeyPressMsg{Code: 'a', Mod: tea.ModCtrl}
)

// fixture: two sessions in an existing dir, one in a removed dir.
func fixture(t *testing.T) (sessions []*session, dir string) {
	t.Helper()
	t.Setenv("HERDR_PLUGIN_STATE_DIR", t.TempDir()) // the scope is remembered: each test starts clean
	dir = t.TempDir()
	now := time.Now()
	return []*session{
		{id: "s1", cwd: dir, title: "Canonical host fix", last: now, pane: "w1:p1"},
		{id: "s2", cwd: filepath.Join(dir, "sub"), prompt: "help pages markup", last: now.Add(-time.Hour)},
		{id: "s3", cwd: filepath.Join(dir, "removed"), title: "Old branch", last: now.Add(-48 * time.Hour), missing: true},
	}, dir
}

func TestFilterKeepsOrderAndMovesCursor(t *testing.T) {
	sessions, _ := fixture(t)
	m := newModel(sessions, "", options{})
	if len(m.rows) != 2 || m.cursor != 0 {
		t.Fatalf("rows = %d, cursor = %d", len(m.rows), m.cursor)
	}
	m = press(m, typed("markup")...)
	if len(m.rows) != 1 || m.current().id != "s2" {
		t.Fatalf("filtered rows = %d, current = %+v", len(m.rows), m.current())
	}
}

func TestToggles(t *testing.T) {
	sessions, dir := fixture(t)
	m := newModel(sessions, "", options{dir: filepath.Join(dir, "sub")})
	// ctrl+a walks the scopes: everywhere, plus missing dirs, this dir, around.
	m = press(m, keyCtrlA)
	if len(m.rows) != 3 || !m.all || m.here {
		t.Errorf("all mode lists %d, want 3", len(m.rows))
	}
	m = press(m, keyCtrlA)
	if len(m.rows) != 1 || m.current().id != "s2" || m.all || !m.here {
		t.Errorf("here mode lists %d rows", len(m.rows))
	}
	m = press(m, keyCtrlA)
	if len(m.rows) != 2 || m.all || m.here {
		t.Errorf("back to default lists %d, want 2", len(m.rows))
	}
	// without a directory to narrow to, it only toggles the missing dirs
	n := press(newModel(sessions, "", options{}), keyCtrlA, keyCtrlA)
	if n.all || n.here || len(n.rows) != 2 {
		t.Errorf("no directory: two presses must be back to default, all=%v here=%v", n.all, n.here)
	}
}

func TestCursorSurvivesToggleWithEmptyQuery(t *testing.T) {
	sessions, _ := fixture(t)
	m := press(newModel(sessions, "", options{}), keyDown)
	if m.current().id != "s2" {
		t.Fatalf("current = %s", m.current().id)
	}
	m = press(m, keyCtrlA)
	if m.current().id != "s2" {
		t.Errorf("cursor jumped to %s", m.current().id)
	}
}

func TestEnterQueuesResume(t *testing.T) {
	sessions, _ := fixture(t)
	next, cmd := newModel(sessions, "", options{}).Update(keyEnter)
	m := next.(model)
	if m.chosen == nil || m.chosen.id != "s1" || cmd == nil {
		t.Fatalf("chosen = %+v, cmd nil = %v", m.chosen, cmd == nil)
	}
}

func TestEnterOnMissingDirStays(t *testing.T) {
	sessions, _ := fixture(t)
	m := press(newModel(sessions, "", options{all: true}), keyDown, keyDown)
	next, cmd := m.Update(keyEnter)
	m = next.(model)
	if m.chosen != nil || cmd != nil || !strings.Contains(m.notice, "no longer exists") {
		t.Errorf("chosen = %+v, notice = %q", m.chosen, m.notice)
	}
}

func TestQQuitsOnlyWithEmptyFilter(t *testing.T) {
	sessions, _ := fixture(t)
	_, cmd := newModel(sessions, "", options{}).Update(typed("q")[0])
	if cmd == nil {
		t.Error("q with an empty filter should quit")
	}
	m := press(newModel(sessions, "", options{}), typed("x")...)
	m = press(m, typed("q")...)
	if m.ti.Value() != "xq" {
		t.Errorf("filter = %q, want q typed as text", m.ti.Value())
	}
}

func TestInitialQuery(t *testing.T) {
	sessions, _ := fixture(t)
	m := newModel(sessions, "", options{query: "canonical"})
	if m.ti.Value() != "canonical" || len(m.rows) != 1 || m.current().id != "s1" {
		t.Errorf("value = %q, rows = %d", m.ti.Value(), len(m.rows))
	}
}

func TestSessionLine(t *testing.T) {
	sessions, _ := fixture(t)
	m := newModel(sessions, "", options{})
	line := ansi.Strip(m.sessionLine(m.rows[0], false, 60))
	if !strings.Contains(line, "●") || !strings.Contains(line, "Canonical host fix") {
		t.Errorf("line = %q", line)
	}
	if w := ansi.StringWidth(ansi.Strip(m.sessionLine(m.rows[0], true, 60))); w != 60 {
		t.Errorf("selected row width = %d, want the full column", w)
	}
}

func TestSelectedRowKeepsItsMatches(t *testing.T) {
	sessions, _ := fixture(t)
	m := newModel(sessions, "", options{query: "canonical"})
	for _, selected := range []bool{false, true} {
		line := m.sessionLine(m.rows[0], selected, 60)
		base := stTitle
		if selected {
			base = stSel
		}
		if want := matchOver(base).Render("C"); !strings.Contains(line, want) {
			t.Errorf("selected %v: no underlined match %q in %q", selected, want, line)
		}
		if w := ansi.StringWidth(line); w != 60 && selected {
			t.Errorf("selected row is %d cells wide, want 60", w)
		}
	}
	if !stMatch.GetUnderline() || matchOver(stSel).GetBackground() != stSel.GetBackground() {
		t.Errorf("a match is underlined and keeps the background it sits on")
	}
}

func TestRenderSession(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "s.jsonl")
	body := `{"type":"user","message":{"content":"first question"}}
{"type":"assistant","message":{"content":[{"type":"text","text":"the answer"},{"type":"tool_use","name":"Bash"}]}}
{"type":"user","message":{"content":[{"type":"tool_result","content":"noise"}]}}
{"type":"user","message":{"content":"<system-reminder>noise</system-reminder>"}}
{"type":"assistant","isSidechain":true,"message":{"content":[{"type":"text","text":"subagent noise"}]}}
`
	if err := os.WriteFile(file, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	s := &session{id: "abc", file: file, cwd: dir, title: "A title", branch: "main", pane: "w1:p1"}
	out := ansi.Strip(renderSession(s, 60, ""))
	for _, want := range []string{"A title", "main · ", "abc", "live in pane w1:p1", "❯ first question", "the answer"} {
		if !strings.Contains(out, want) {
			t.Errorf("preview lacks %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "noise") {
		t.Errorf("preview shows harness or subagent text:\n%s", out)
	}
}

func TestReadTailCapsTurns(t *testing.T) {
	var b strings.Builder
	for i := 0; i < previewMaxTurns+5; i++ {
		b.WriteString(`{"type":"user","message":{"content":"q"}}` + "\n")
	}
	file := filepath.Join(t.TempDir(), "s.jsonl")
	if err := os.WriteFile(file, []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	turns, cut, err := readTail(file)
	if err != nil || len(turns) != previewMaxTurns || !cut {
		t.Errorf("turns = %d, cut = %v, err = %v", len(turns), cut, err)
	}
}

// TestFrameGeometry pins the single-frame layout: exactly height lines, each
// exactly width cells, with the sections where the click math expects them.
func TestFrameGeometry(t *testing.T) {
	sessions, _ := fixture(t)
	for _, size := range [][2]int{{94, 24}, {150, 16}, {61, 12}} {
		next, _ := newModel(sessions, "", options{}).Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
		m := next.(model)
		lines := strings.Split(m.render(), "\n")
		if len(lines) != size[1] {
			t.Errorf("%v: %d lines, want %d", size, len(lines), size[1])
		}
		for i, l := range lines {
			if w := ansi.StringWidth(l); w != size[0] {
				t.Errorf("%v: line %d is %d cells, want %d: %q", size, i, w, size[0], ansi.Strip(l))
			}
		}
		plain := strings.Split(ansi.Strip(m.render()), "\n")
		if !strings.HasPrefix(plain[0], "╭") || !strings.HasPrefix(plain[len(plain)-1], "╰") || !strings.HasPrefix(plain[1], "│ asgotosession") {
			t.Errorf("%v: frame corners missing", size)
		}
		if !strings.Contains(plain[len(plain)-3], "─ 2/2 ─┴") || !strings.Contains(plain[mainY(false)], "┬") {
			t.Errorf("%v: counter or divider misplaced:\n%s\n%s", size, plain[len(plain)-3], plain[mainY(false)])
		}
		if !strings.Contains(plain[listY(false)], "▌●Canon") {
			t.Errorf("%v: first row is not at listY: %q", size, plain[listY(false)])
		}
	}
}

func TestClickSelectsRow(t *testing.T) {
	sessions, _ := fixture(t)
	m := newModel(sessions, "", options{})
	next, _ := m.Update(tea.MouseClickMsg{X: 3, Y: listY(false) + 1, Button: tea.MouseLeft})
	clicked := next.(model)
	if got := clicked.current().id; got != "s2" {
		t.Errorf("click on the second row selected %s", got)
	}
	// The divider, the preview and the frame's own lines select nothing.
	for _, c := range [][2]int{{0, listY(false) + 1}, {m.listW() + 1, listY(false) + 1}, {3, mainY(false)}, {3, 1}} {
		next, _ = m.Update(tea.MouseClickMsg{X: c[0], Y: c[1], Button: tea.MouseLeft})
		clicked = next.(model)
		if got := clicked.current().id; got != "s1" {
			t.Errorf("click at %v moved the cursor to %s", c, got)
		}
	}
}

// TestStatusCarriesTheScope: there is no context line; what the list is
// narrowed or widened to sits on the top border, the counter under the list.
func TestStatusCarriesTheScope(t *testing.T) {
	sessions, dir := fixture(t)
	m := newModel(sessions, "", options{dir: dir})
	if c, s := ansi.Strip(m.counter()), m.status(); c != "2/2" || s != "" {
		t.Errorf("default counter = %q, status = %q", c, s)
	}
	// both at once only come from the flags: ctrl+a walks one scope at a time
	m = newModel(sessions, "", options{dir: dir, here: true, all: true})
	if c := ansi.Strip(m.counter()); c != "3/3" {
		t.Errorf("scoped counter = %q", c)
	}
	if s := ansi.Strip(m.status()); !strings.HasPrefix(s, "[in ") || !strings.HasSuffix(s, ", +missing dirs]") {
		t.Errorf("scope = %q", s)
	}
	plain := strings.Split(ansi.Strip(m.render()), "\n")
	if top := plain[0]; !strings.HasPrefix(top, "╭") || !strings.Contains(top, " [in ") || strings.Contains(top, "3/3") {
		t.Errorf("top border = %q", top)
	}
	if edge := plain[len(plain)-3]; !strings.Contains(edge, "─ 3/3 ─┴") {
		t.Errorf("edge under the list = %q", edge)
	}
}

// TestMain sandboxes the state dir: tests must never touch the real one.
func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "asgotosession-test")
	if err != nil {
		panic(err)
	}
	os.Setenv("HERDR_PLUGIN_STATE_DIR", dir)
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}

// TestResizeList: shift+arrows move the divider, the frame still fits, and
// the position is there for the next run.
func TestResizeList(t *testing.T) {
	t.Setenv("HERDR_PLUGIN_STATE_DIR", t.TempDir())
	sessions, _ := fixture(t)
	next, _ := newModel(sessions, "", options{}).Update(tea.WindowSizeMsg{Width: 120, Height: 24})
	m := next.(model)
	m.split = splitDefault
	m.resize()
	w := m.listW()
	shift := func(code rune) {
		next, _ := m.Update(tea.KeyPressMsg{Code: code, Mod: tea.ModShift})
		m = next.(model)
	}

	shift(tea.KeyRight)
	if m.listW() <= w || m.split != splitDefault-splitStep || loadSplit(stateDir()) != m.split {
		t.Errorf("grow: list %d -> %d, split=%d, saved=%d", w, m.listW(), m.split, loadSplit(stateDir()))
	}
	if m.listVP.Width() != m.listW() || m.prevVP.Width() != m.prevW() {
		t.Errorf("viewports %d | %d, want %d | %d", m.listVP.Width(), m.prevVP.Width(), m.listW(), m.prevW())
	}
	for i, l := range strings.Split(m.View().Content, "\n") {
		if got := ansi.StringWidth(l); got != m.width {
			t.Errorf("line %d is %d cells after the resize, want %d", i, got, m.width)
		}
	}

	shift(tea.KeyLeft)
	shift(tea.KeyLeft)
	if m.listW() >= w || m.split != splitDefault+splitStep {
		t.Errorf("shrink: list %d -> %d, split=%d", w, m.listW(), m.split)
	}
	for range 10 {
		shift(tea.KeyLeft)
	}
	if m.split != splitMax {
		t.Errorf("split should clamp at %d, got %d", splitMax, m.split)
	}
}

// TestMouseWheelFollowsThePointer: over the list the wheel moves the
// selection, as in asgitlog; anywhere else it scrolls the preview.
func TestMouseWheelFollowsThePointer(t *testing.T) {
	sessions, _ := fixture(t)
	next, _ := newModel(sessions, "", options{}).Update(tea.WindowSizeMsg{Width: 120, Height: 24})
	m := next.(model)
	wheel := func(x int, b tea.MouseButton) {
		next, _ := m.Update(tea.MouseWheelMsg{X: x, Y: listY(false), Button: b})
		m = next.(model)
	}
	first := m.cursor
	wheel(2, tea.MouseWheelDown)
	if m.cursor <= first {
		t.Errorf("wheel down over the list: cursor %d -> %d", first, m.cursor)
	}
	wheel(2, tea.MouseWheelUp)
	if m.cursor != first {
		t.Errorf("wheel up over the list: cursor = %d, want %d", m.cursor, first)
	}
	m.prevVP.SetContent(strings.Repeat("line\n", 200))
	wheel(m.listW()+10, tea.MouseWheelDown)
	if m.cursor != first || m.prevVP.YOffset() == 0 {
		t.Errorf("wheel over the preview: cursor = %d, preview at %d", m.cursor, m.prevVP.YOffset())
	}
}

// TestPanel: f1 lays the scope and the keys over a frame that keeps its
// size, takes every key while it is open, and esc closes it before it quits.
// `?` is text for the filter.
func TestPanel(t *testing.T) {
	sessions, dir := fixture(t)
	m := newModel(sessions, "", options{dir: filepath.Join(dir, "sub")})
	res, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 24})
	m = res.(model)
	lines := func(m model) []string { return strings.Split(ansi.Strip(m.render()), "\n") }
	closed := lines(m)
	if len(closed) != 24 || !strings.Contains(closed[22], "f1 options") || strings.Contains(closed[22], "pgup") {
		t.Fatalf("help line = %q (%d lines)", closed[22], len(closed))
	}
	list := m.listVP.Height()
	m = press(m, tea.KeyPressMsg{Code: tea.KeyF1})
	open := lines(m)
	all := strings.Join(open, "\n")
	if len(open) != 24 || m.listVP.Height() != list {
		t.Fatalf("the panel changed the frame: %d lines, list %d -> %d", len(open), list, m.listVP.Height())
	}
	for _, want := range []string{"╭─ options ", "▌ Sessions", "this dir", "‹everywhere›", "+ missing dirs", "^a", "Keys", "pgup/pgdn", "scroll preview", "esc close"} {
		if !strings.Contains(all, want) {
			t.Errorf("the panel lacks %q:\n%s", want, all)
		}
	}
	for i, l := range open {
		if ansi.StringWidth(l) != 120 {
			t.Errorf("line %d is %d cells wide, want 120", i, ansi.StringWidth(l))
		}
	}
	m = press(m, tea.KeyPressMsg{Code: 'z', Text: "z"}, tea.KeyPressMsg{Code: tea.KeySpace, Text: " "})
	if m.ti.Value() != "" || m.here || !m.all {
		t.Errorf("space moves to the missing dirs and nothing reaches the filter: %q here=%v all=%v", m.ti.Value(), m.here, m.all)
	}
	m = press(m, tea.KeyPressMsg{Code: tea.KeyLeft}, tea.KeyPressMsg{Code: tea.KeyLeft})
	if !m.here || m.all || !strings.Contains(ansi.Strip(m.render()), "‹this dir›") {
		t.Errorf("left twice narrows to this dir: here=%v all=%v", m.here, m.all)
	}
	res, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	m = res.(model)
	if m.panel.open || cmd != nil {
		t.Errorf("esc closes the panel and nothing else: open=%v cmd=%v", m.panel.open, cmd)
	}
	m = press(m, tea.KeyPressMsg{Code: 'x', Text: "x"}, tea.KeyPressMsg{Code: '?', Text: "?"})
	if m.panel.open || m.ti.Value() != "x?" {
		t.Errorf("filter = %q, panel open = %v, want ? typed as text", m.ti.Value(), m.panel.open)
	}
	// Without a directory to narrow to, the scope has two values.
	bare := newModel(sessions, "", options{})
	if got := bare.options()[0].values; len(got) != 2 || got[0] != scopeAll {
		t.Errorf("scopes without a directory = %q", got)
	}
}

// TestCopyKeyCopiesTheSessionID covers ctrl+y: the id of the session under
// the cursor goes to the clipboard, the help line confirms it for a moment,
// and the filter is left alone.
func TestCopyKeyCopiesTheSessionID(t *testing.T) {
	log := filepath.Join(t.TempDir(), "clip")
	stub := filepath.Join(t.TempDir(), "clipboard")
	if err := os.WriteFile(stub, []byte("#!/bin/sh\ncat > "+log+"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("ASGOTOSESSION_CLIPBOARD", stub)
	sessions, _ := fixture(t)
	m := press(newModel(sessions, "", options{}), keyDown)
	res, cmd := m.Update(tea.KeyPressMsg{Code: 'y', Mod: tea.ModCtrl})
	if cmd == nil {
		t.Fatal("ctrl+y returned no command")
	}
	res, _ = res.(model).Update(cmd())
	m = res.(model)
	if got, _ := os.ReadFile(log); string(got) != "s2" {
		t.Errorf("the clipboard got %q, want the id of the session under the cursor", got)
	}
	plain := strings.Split(ansi.Strip(m.View().Content), "\n")
	if help := plain[len(plain)-2]; !strings.Contains(help, "copied s2") {
		t.Errorf("help line = %q, want the confirmation", help)
	}
	if m.ti.Value() != "" {
		t.Errorf("ctrl+y leaked into the filter: %q", m.ti.Value())
	}
	res, _ = m.Update(clearFlashMsg(m.flash.seq))
	plain = strings.Split(ansi.Strip(res.(model).View().Content), "\n")
	if help := plain[len(plain)-2]; !strings.Contains(help, "type filter") {
		t.Errorf("after the timer the help is back: %q", help)
	}

	// with nothing under the cursor there is nothing to copy
	m = press(newModel(sessions, "", options{}), typed("zzzz")...)
	res, cmd = m.Update(tea.KeyPressMsg{Code: 'y', Mod: tea.ModCtrl})
	res, _ = res.(model).Update(cmd())
	if got := res.(model).flash.text; got != "nothing to copy" {
		t.Errorf("flash with an empty list = %q", got)
	}
}

// TestScopeIsRemembered: the scope is a setting, kept for the next run; the
// flags of the command line go before it.
func TestScopeIsRemembered(t *testing.T) {
	sessions, dir := fixture(t)
	sub := options{dir: filepath.Join(dir, "sub")}
	m := press(newModel(sessions, "", sub), keyCtrlA) // everywhere -> + missing dirs
	if !m.all || m.here || loadSetting(stateDir(), "scope") != "missing" {
		t.Fatalf("ctrl+a: here=%v all=%v saved=%q", m.here, m.all, loadSetting(stateDir(), "scope"))
	}
	if next := newModel(sessions, "", sub); !next.all || next.here {
		t.Errorf("the next run lists the missing dirs again: here=%v all=%v", next.here, next.all)
	}
	m = press(m, keyCtrlA) // -> this dir
	if next := newModel(sessions, "", sub); !next.here || next.all {
		t.Errorf("the next run narrows to this dir again: here=%v all=%v", next.here, next.all)
	}
	if next := newModel(sessions, "", options{}); next.here {
		t.Errorf("without a directory there is nothing to narrow to")
	}
	if next := newModel(sessions, "", options{dir: sub.dir, all: true}); next.here || !next.all {
		t.Errorf("-all goes before the remembered scope: here=%v all=%v", next.here, next.all)
	}
}
