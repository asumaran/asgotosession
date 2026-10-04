package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
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

func TestReadTranscript(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("CLAUDE_PROJECTS_DIR", dir)
	if err := os.MkdirAll(filepath.Join(dir, "-home-u-shop"), 0o755); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(dir, "-home-u-shop", "abc-123.jsonl")
	if err := os.WriteFile(file, []byte(sampleTranscript), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := transcriptPath("abc-123"); got != file {
		t.Errorf("transcriptPath = %q, want %q", got, file)
	}
	got, ok := readTranscript("abc-123")
	if !ok || got.Title != "Second title" || got.Branch != "fix/ESHOP-1" {
		t.Errorf("readTranscript = %+v, %v", got, ok)
	}
	for _, id := range []string{"", "missing", "*", "../x"} {
		if _, ok := readTranscript(id); ok {
			t.Errorf("readTranscript(%q) found a transcript", id)
		}
	}
}
