package main

import (
	"errors"
	"strings"
	"testing"
)

// fakeHerdr answers CLI calls from a table keyed by the first two words and
// records every call.
type fakeHerdr struct {
	replies map[string]string
	calls   []string
}

func (f *fakeHerdr) run(args ...string) ([]byte, error) {
	call := strings.Join(args, " ")
	f.calls = append(f.calls, call)
	for prefix, out := range f.replies {
		if strings.HasPrefix(call, prefix) {
			if out == "ERR" {
				return nil, errors.New("herdr failed")
			}
			return []byte(out), nil
		}
	}
	return []byte(`{}`), nil
}

func (f *fakeHerdr) called(prefix string) bool {
	for _, c := range f.calls {
		if strings.HasPrefix(c, prefix) {
			return true
		}
	}
	return false
}

const (
	panesJSON = `{"result":{"panes":[
		{"pane_id":"w1:p1","workspace_id":"w1","cwd":"/repo","agent":"claude","agent_session":{"value":"live-id"}},
		{"pane_id":"w1:p2","workspace_id":"w1","cwd":"/repo","agent":null},
		{"pane_id":"w2:p1","workspace_id":"w2","cwd":"/other","agent":null}
	]}}`
	spacesJSON = `{"result":{"workspaces":[
		{"workspace_id":"w1","worktree":{"checkout_path":"/repo"}},
		{"workspace_id":"w3","worktree":null}
	]}}`
	idleJSON  = `{"result":{"process_info":{"foreground_process_group_id":10,"shell_pid":10}}}`
	busyJSON  = `{"result":{"process_info":{"foreground_process_group_id":99,"shell_pid":10}}}`
	newTabOut = `{"result":{"root_pane":{"pane_id":"w1:p9"}}}`
)

func TestOpenLiveSessionJustFocuses(t *testing.T) {
	f := &fakeHerdr{replies: map[string]string{"pane list": panesJSON}}
	pane, err := openInHerdr(f.run, "live-id", "/repo")
	if err != nil || pane != "" {
		t.Fatalf("pane = %q, err = %v", pane, err)
	}
	if !f.called("agent focus w1:p1") || f.called("pane run") {
		t.Errorf("calls = %v", f.calls)
	}
}

func TestOpenReusesIdleShell(t *testing.T) {
	f := &fakeHerdr{replies: map[string]string{
		"pane list": panesJSON, "workspace list": spacesJSON, "pane process-info": idleJSON,
	}}
	pane, err := openInHerdr(f.run, "sid", "/repo")
	if err != nil || pane != "w1:p2" {
		t.Fatalf("pane = %q, err = %v", pane, err)
	}
	if !f.called("workspace focus w1") || f.called("tab create") || !f.called("pane run w1:p2 claude --resume sid") {
		t.Errorf("calls = %v", f.calls)
	}
}

func TestOpenCreatesTabWhenShellBusy(t *testing.T) {
	f := &fakeHerdr{replies: map[string]string{
		"pane list": panesJSON, "workspace list": spacesJSON, "pane process-info": busyJSON,
		"tab create": newTabOut,
	}}
	pane, err := openInHerdr(f.run, "sid", "/repo")
	if err != nil || pane != "w1:p9" {
		t.Fatalf("pane = %q, err = %v", pane, err)
	}
	if !f.called("tab create --workspace w1 --cwd /repo --label claude --focus") || !f.called("pane run w1:p9") {
		t.Errorf("calls = %v", f.calls)
	}
}

func TestOpenFindsSpaceByPaneCwd(t *testing.T) {
	// /other has no worktree-backed workspace; a pane sitting there decides.
	f := &fakeHerdr{replies: map[string]string{
		"pane list": panesJSON, "workspace list": spacesJSON, "pane process-info": idleJSON,
	}}
	pane, err := openInHerdr(f.run, "sid", "/other")
	if err != nil || pane != "w2:p1" {
		t.Fatalf("pane = %q, err = %v", pane, err)
	}
	if !f.called("workspace focus w2") {
		t.Errorf("calls = %v", f.calls)
	}
}

func TestOpenCreatesSpace(t *testing.T) {
	f := &fakeHerdr{replies: map[string]string{
		"pane list": panesJSON, "workspace list": spacesJSON, "workspace create": newTabOut,
	}}
	pane, err := openInHerdr(f.run, "sid", "/brand/new-dir")
	if err != nil || pane != "w1:p9" {
		t.Fatalf("pane = %q, err = %v", pane, err)
	}
	if !f.called("workspace create --cwd /brand/new-dir --label new-dir --focus") || f.called("workspace focus") {
		t.Errorf("calls = %v", f.calls)
	}
}

func TestOpenReportsErrors(t *testing.T) {
	f := &fakeHerdr{replies: map[string]string{"pane list": "ERR"}}
	if _, err := openInHerdr(f.run, "sid", "/repo"); err == nil {
		t.Error("want an error when pane list fails")
	}
	f = &fakeHerdr{replies: map[string]string{
		"pane list": panesJSON, "workspace list": spacesJSON, "workspace create": `{"result":{}}`,
	}}
	if _, err := openInHerdr(f.run, "sid", "/nowhere"); err == nil {
		t.Error("want an error when herdr reports no pane")
	}
}

func TestLiveSessions(t *testing.T) {
	f := &fakeHerdr{replies: map[string]string{"pane list": panesJSON}}
	live := liveSessions(f.run)
	if len(live) != 1 || live["live-id"] != "w1:p1" {
		t.Errorf("live = %v", live)
	}
	f = &fakeHerdr{replies: map[string]string{"pane list": "ERR"}}
	if got := liveSessions(f.run); len(got) != 0 {
		t.Errorf("live = %v, want none on error", got)
	}
}

func TestResumeCommand(t *testing.T) {
	t.Setenv("ASGOTOSESSION_OPENER", "")
	if got := resumeCommand("abc"); got != "claude --resume abc" {
		t.Errorf("got %q", got)
	}
	t.Setenv("ASGOTOSESSION_OPENER", "myclaude  --flag")
	if got := resumeCommand("abc"); got != "myclaude --flag --resume abc" {
		t.Errorf("got %q", got)
	}
}
