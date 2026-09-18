package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const sampleTranscript = `{"type":"mode","mode":"default"}
{"type":"user","cwd":"/home/u/wt/shop/fix-ESHOP-1","gitBranch":"fix/ESHOP-1","isSidechain":false,"message":{"role":"user","content":"<local-command-caveat>ignore</local-command-caveat>"}}
{"type":"user","cwd":"/home/u/elsewhere","isSidechain":false,"message":{"role":"user","content":"fix the   canonical\nurl"}}
{"type":"ai-title","aiTitle":"First title","sessionId":"x"}
{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"On it."},{"type":"tool_use","name":"Bash"}]}}
{"type":"ai-title","aiTitle":"Second title","sessionId":"x"}
`

func TestScanTranscript(t *testing.T) {
	got := scanTranscript(strings.NewReader(sampleTranscript))
	if got.Cwd != "/home/u/wt/shop/fix-ESHOP-1" {
		t.Errorf("cwd = %q, want the first one recorded", got.Cwd)
	}
	if got.Branch != "fix/ESHOP-1" {
		t.Errorf("branch = %q", got.Branch)
	}
	if got.Title != "Second title" {
		t.Errorf("title = %q, want the latest ai title", got.Title)
	}
	if got.Prompt != "fix the canonical url" {
		t.Errorf("prompt = %q, want the first typed prompt on one line", got.Prompt)
	}
}

func TestScanTranscriptCustomTitleWins(t *testing.T) {
	in := `{"type":"custom-title","customTitle":"Mine","sessionId":"x"}` + "\n" +
		`{"type":"ai-title","aiTitle":"Generated later","sessionId":"x"}` + "\n"
	if got := scanTranscript(strings.NewReader(in)).Title; got != "Mine" {
		t.Errorf("title = %q, want the custom one", got)
	}
}

func TestScanTranscriptNoTrailingNewline(t *testing.T) {
	in := `{"type":"user","cwd":"/a","message":{"content":"hi"}}`
	if got := scanTranscript(strings.NewReader(in)); got.Cwd != "/a" || got.Prompt != "hi" {
		t.Errorf("got %+v", got)
	}
}

func TestPromptFromBlocks(t *testing.T) {
	line := `{"type":"user","message":{"content":[{"type":"text","text":"<system-reminder>x</system-reminder>"},{"type":"text","text":"real prompt"}]}}`
	if got := promptOf([]byte(line)); got != "real prompt" {
		t.Errorf("prompt = %q", got)
	}
	side := `{"type":"user","isSidechain":true,"message":{"content":"subagent brief"}}`
	if got := promptOf([]byte(side)); got != "" {
		t.Errorf("sidechain prompt = %q, want none", got)
	}
	result := `{"type":"user","message":{"content":[{"type":"tool_result","content":"out"}]}}`
	if got := promptOf([]byte(result)); got != "" {
		t.Errorf("tool result prompt = %q, want none", got)
	}
}

func TestJSONStringAfter(t *testing.T) {
	line := []byte(`{"cwd":"/tmp/a \"quoted\" dir\\x","gitBranch":"main"}`)
	if got, ok := jsonStringAfter(line, cwdKey); !ok || got != `/tmp/a "quoted" dir\x` {
		t.Errorf("cwd = %q, %v", got, ok)
	}
	if _, ok := jsonStringAfter([]byte(`{"cwd":"unterminated`), cwdKey); ok {
		t.Error("unterminated string decoded")
	}
	// An escaped key inside a message body is not a key.
	if _, ok := jsonStringAfter([]byte(`{"text":"\"cwd\":\"/fake\""}`), cwdKey); ok {
		t.Error("matched a key inside a string")
	}
}

// writeTranscript creates <dir>/<project>/<id>.jsonl with the given mtime.
func writeTranscript(t *testing.T, dir, project, id, body string, mtime time.Time) string {
	t.Helper()
	p := filepath.Join(dir, project)
	if err := os.MkdirAll(p, 0o755); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(p, id+".jsonl")
	if err := os.WriteFile(file, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(file, mtime, mtime); err != nil {
		t.Fatal(err)
	}
	return file
}

func userLine(cwd, text string) string {
	return `{"type":"user","cwd":"` + cwd + `","message":{"content":"` + text + `"}}` + "\n"
}

func TestLoadSessions(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "projects")
	alive := filepath.Join(root, "alive")
	if err := os.Mkdir(alive, 0o755); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	writeTranscript(t, dir, "-a", "old", userLine(alive, "old one"), now.Add(-2*time.Hour))
	writeTranscript(t, dir, "-a", "new", userLine(alive, "new one"), now.Add(-time.Minute))
	writeTranscript(t, dir, "-b", "gone", userLine(filepath.Join(root, "removed"), "x"), now.Add(-time.Hour))
	writeTranscript(t, dir, "-b", "empty", `{"type":"mode"}`+"\n", now)

	cache := filepath.Join(root, "state", "sessions.json")
	sessions, err := loadSessions(dir, cache)
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, s := range sessions {
		ids = append(ids, s.id)
	}
	if got := strings.Join(ids, ","); got != "new,gone,old" {
		t.Fatalf("ids = %s, want newest first without the cwd-less one", got)
	}
	if !sessions[1].missing || sessions[0].missing {
		t.Errorf("missing flags wrong: %+v %+v", sessions[0], sessions[1])
	}

	if got := len(visibleSessions(sessions, false, "")); got != 2 {
		t.Errorf("default mode lists %d, want 2", got)
	}
	if got := len(visibleSessions(sessions, true, "")); got != 3 {
		t.Errorf("all mode lists %d, want 3", got)
	}
	if got := len(visibleSessions(sessions, true, alive)); got != 2 {
		t.Errorf("here mode lists %d, want 2", got)
	}

	// The cache answers for unchanged files: a rewrite that keeps mtime and
	// size is not read again, one that changes them is.
	if len(loadCache(cache)) != 4 {
		t.Fatalf("cache has %d entries, want 4", len(loadCache(cache)))
	}
	file := filepath.Join(dir, "-a", "new.jsonl")
	st, _ := os.Stat(file)
	writeTranscript(t, dir, "-a", "new", userLine(alive, "NEW ONE"), st.ModTime())
	sessions, _ = loadSessions(dir, cache)
	if sessions[0].prompt != "new one" {
		t.Errorf("prompt = %q, want the cached one", sessions[0].prompt)
	}
	writeTranscript(t, dir, "-a", "new", userLine(alive, "rewritten"), now)
	sessions, _ = loadSessions(dir, cache)
	if sessions[0].prompt != "rewritten" {
		t.Errorf("prompt = %q, want the rescanned one", sessions[0].prompt)
	}
}

func TestLoadSessionsNoDir(t *testing.T) {
	if _, err := loadSessions(filepath.Join(t.TempDir(), "nope"), ""); err == nil {
		t.Error("want an error for a missing projects dir")
	}
}

func TestInsideDir(t *testing.T) {
	cases := []struct {
		dir, path string
		want      bool
	}{
		{"/a/b", "/a/b", true},
		{"/a/b", "/a/b/c", true},
		{"/a/b", "/a/bc", false},
		{"/a/b/", "/a/b/c", true},
		{"/a/b", "/a", false},
	}
	for _, c := range cases {
		if got := insideDir(c.dir, c.path); got != c.want {
			t.Errorf("insideDir(%q, %q) = %v", c.dir, c.path, got)
		}
	}
}

func TestLabel(t *testing.T) {
	if got := (&session{title: "T", prompt: "P"}).label(); got != "T" {
		t.Errorf("label = %q", got)
	}
	if got := (&session{prompt: "P"}).label(); got != "P" {
		t.Errorf("label = %q", got)
	}
	if got := (&session{}).label(); got != "(untitled)" {
		t.Errorf("label = %q", got)
	}
}

func TestCompactAge(t *testing.T) {
	now := time.Now()
	for d, want := range map[time.Duration]string{
		30 * time.Second: "0m", 90 * time.Minute: "1h", 50 * time.Hour: "2d", 15 * 24 * time.Hour: "2w",
	} {
		if got := compactAge(now.Add(-d), now); got != want {
			t.Errorf("compactAge(%s) = %q, want %q", d, got, want)
		}
	}
}
