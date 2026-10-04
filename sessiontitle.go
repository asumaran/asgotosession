package main

// What a Claude Code transcript says about its session: the directory it was
// started in, its branch, its title (a custom title, else the latest ai
// title) and the first prompt the user typed. A transcript is
// <projects>/<project>/<session id>.jsonl, under ~/.claude/projects or
// CLAUDE_PROJECTS_DIR.

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// projectsDir is where Claude Code keeps the transcripts.
func projectsDir() string {
	if p := os.Getenv("CLAUDE_PROJECTS_DIR"); p != "" {
		return p
	}
	return filepath.Join(homeDir(), ".claude", "projects")
}

// scanned is what one pass over a transcript yields.
type scanned struct {
	Cwd    string `json:"cwd"`
	Branch string `json:"branch,omitempty"`
	Title  string `json:"title,omitempty"`
	Prompt string `json:"prompt,omitempty"`
}

var (
	cwdKey      = []byte(`"cwd":"`)
	branchKey   = []byte(`"gitBranch":"`)
	aiTitlePre  = []byte(`{"type":"ai-title"`)
	customPre   = []byte(`{"type":"custom-title"`)
	userTypeKey = []byte(`"type":"user"`)
)

const promptMaxRunes = 200

// scanTranscript reads a transcript line by line. Lines are matched on raw
// bytes and only the few that matter are decoded: transcripts run to tens of
// megabytes and almost every line is a message body.
func scanTranscript(r io.Reader) scanned {
	var out scanned
	custom := false
	br := bufio.NewReaderSize(r, 256*1024)
	for {
		line, err := br.ReadBytes('\n')
		if len(line) > 0 {
			switch {
			case bytes.HasPrefix(line, customPre):
				// A custom title always wins; otherwise keep the latest ai title.
				if t := titleOf(line); t != "" {
					out.Title, custom = t, true
				}
			case bytes.HasPrefix(line, aiTitlePre):
				if t := titleOf(line); t != "" && !custom {
					out.Title = t
				}
			default:
				if out.Cwd == "" {
					if v, ok := jsonStringAfter(line, cwdKey); ok {
						out.Cwd = v
						out.Branch, _ = jsonStringAfter(line, branchKey)
					}
				}
				if out.Prompt == "" && bytes.Contains(line, userTypeKey) {
					out.Prompt = promptOf(line)
				}
			}
		}
		if err != nil {
			return out
		}
	}
}

func titleOf(line []byte) string {
	var t struct {
		AiTitle     string `json:"aiTitle"`
		CustomTitle string `json:"customTitle"`
	}
	if json.Unmarshal(line, &t) != nil {
		return ""
	}
	if t.CustomTitle != "" {
		return oneLine(t.CustomTitle)
	}
	return oneLine(t.AiTitle)
}

// jsonStringAfter decodes the JSON string that follows key in line.
func jsonStringAfter(line, key []byte) (string, bool) {
	i := bytes.Index(line, key)
	if i < 0 {
		return "", false
	}
	start := i + len(key) - 1 // the opening quote
	for j := start + 1; j < len(line); j++ {
		switch line[j] {
		case '\\':
			j++
		case '"':
			var s string
			if json.Unmarshal(line[start:j+1], &s) != nil {
				return "", false
			}
			return s, true
		}
	}
	return "", false
}

// message is the part of a transcript line the prompt and the preview need.
type message struct {
	Type        string `json:"type"`
	IsSidechain bool   `json:"isSidechain"`
	Message     struct {
		Content json.RawMessage `json:"content"`
	} `json:"message"`
}

// texts returns the text blocks of a message: the content is either a plain
// string or a list of typed blocks.
func (m *message) texts() []string {
	var s string
	if json.Unmarshal(m.Message.Content, &s) == nil {
		return []string{s}
	}
	var blocks []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if json.Unmarshal(m.Message.Content, &blocks) != nil {
		return nil
	}
	var out []string
	for _, b := range blocks {
		if b.Type == "text" && b.Text != "" {
			out = append(out, b.Text)
		}
	}
	return out
}

// typedByUser reports whether a user text is something the person wrote:
// harness entries (command output, reminders, caveats) are wrapped in tags.
func typedByUser(text string) bool {
	t := strings.TrimSpace(text)
	return t != "" && !strings.HasPrefix(t, "<")
}

func promptOf(line []byte) string {
	var m message
	if json.Unmarshal(line, &m) != nil || m.Type != "user" || m.IsSidechain {
		return ""
	}
	for _, t := range m.texts() {
		if typedByUser(t) {
			r := []rune(oneLine(t))
			if len(r) > promptMaxRunes {
				r = r[:promptMaxRunes]
			}
			return string(r)
		}
	}
	return ""
}

// oneLine collapses whitespace so a title never breaks the row.
func oneLine(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

// transcriptPath finds the transcript of a session id under the projects
// dir; "" when there is none.
func transcriptPath(id string) string {
	if id == "" || strings.ContainsAny(id, `/\*?[`) {
		return ""
	}
	m, _ := filepath.Glob(filepath.Join(projectsDir(), "*", id+".jsonl"))
	if len(m) == 0 {
		return ""
	}
	return m[0]
}

// readTranscript scans the transcript of a session id; false when there is
// none or it cannot be opened.
func readTranscript(id string) (scanned, bool) {
	p := transcriptPath(id)
	if p == "" {
		return scanned{}, false
	}
	f, err := os.Open(p)
	if err != nil {
		return scanned{}, false
	}
	defer f.Close()
	return scanTranscript(f), true
}
