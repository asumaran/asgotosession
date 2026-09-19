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
	m = press(m, keyCtrlA)
	if len(m.rows) != 3 {
		t.Errorf("all mode lists %d, want 3", len(m.rows))
	}
	m = press(m, keyTab)
	if len(m.rows) != 1 || m.current().id != "s2" {
		t.Errorf("here mode lists %d rows", len(m.rows))
	}
	m = press(m, keyTab, keyCtrlA)
	if len(m.rows) != 2 {
		t.Errorf("back to default lists %d, want 2", len(m.rows))
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

func TestPathCellsKeepsTail(t *testing.T) {
	got := ansi.Strip(pathCells("~/wt/monorepo-front/fix-ESHOP-551", nil, 14, false))
	if got != "…fix-ESHOP-551" {
		t.Errorf("got %q", got)
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
		if !strings.Contains(plain[0], "2/2") || !strings.Contains(plain[mainY(false)], "┬") {
			t.Errorf("%v: counter or divider misplaced:\n%s\n%s", size, plain[0], plain[mainY(false)])
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

// TestCounterCarriesTheScope: there is no context line; what the list is
// narrowed or widened to sits next to the counter, on the top border.
func TestCounterCarriesTheScope(t *testing.T) {
	sessions, dir := fixture(t)
	m := newModel(sessions, "", options{dir: dir})
	if c := ansi.Strip(m.counter()); c != "2/2" {
		t.Errorf("default counter = %q", c)
	}
	m = press(m, keyTab, keyCtrlA)
	c := ansi.Strip(m.counter())
	if !strings.HasPrefix(c, "3/3 [in ") || !strings.HasSuffix(c, ", +missing dirs]") {
		t.Errorf("scoped counter = %q", c)
	}
	if top := strings.Split(ansi.Strip(m.render()), "\n")[0]; !strings.HasPrefix(top, "╭") || !strings.Contains(top, "3/3 [in ") {
		t.Errorf("top border = %q", top)
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
