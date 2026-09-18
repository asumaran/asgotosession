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
	got := pathCells("~/wt/monorepo-front/fix-ESHOP-551", nil, 14, false)
	if got != "…fix-ESHOP-551" {
		t.Errorf("got %q", got)
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
