package main

// Pure logic over the Claude Code transcripts: one <project>/<id>.jsonl per
// session under ~/.claude/projects. A transcript is scanned once for the
// directory it must be resumed from, its branch, its title and its first
// prompt; the result is cached on disk per (path, mtime, size) so reopening
// the popup only reads the transcripts that changed.

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"
)

// projectsDir is where Claude Code keeps the transcripts.
func projectsDir() string {
	if p := os.Getenv("CLAUDE_PROJECTS_DIR"); p != "" {
		return p
	}
	return filepath.Join(homeDir(), ".claude", "projects")
}

func homeDir() string {
	h, _ := os.UserHomeDir()
	return h
}

// session is one resumable transcript.
type session struct {
	id      string
	file    string
	cwd     string // first cwd recorded: the directory to resume from
	branch  string
	title   string // custom title, else the latest ai title
	prompt  string // first prompt typed by the user, shown when untitled
	last    time.Time
	missing bool   // cwd no longer exists
	pane    string // herdr pane where the session is live, if any
}

// label is what the title column shows.
func (s *session) label() string {
	switch {
	case s.title != "":
		return s.title
	case s.prompt != "":
		return s.prompt
	}
	return "(untitled)"
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

// ---- disk cache ----

type cacheEntry struct {
	MTime int64 `json:"mtime"`
	Size  int64 `json:"size"`
	scanned
}

// stateDir is the herdr-injected per-plugin state dir; standalone runs fall
// back to a fixed path under ~/.config/herdr.
func stateDir() string {
	if dir := os.Getenv("HERDR_PLUGIN_STATE_DIR"); dir != "" {
		return dir
	}
	base := os.Getenv("XDG_CONFIG_HOME")
	if base == "" {
		base = filepath.Join(homeDir(), ".config")
	}
	return filepath.Join(base, "herdr", "gotosession-tui")
}

func cacheFile() string {
	return filepath.Join(stateDir(), "sessions.json")
}

func loadCache(path string) map[string]cacheEntry {
	c := map[string]cacheEntry{}
	if data, err := os.ReadFile(path); err == nil {
		_ = json.Unmarshal(data, &c)
	}
	return c
}

func saveCache(path string, c map[string]cacheEntry) {
	if data, err := json.Marshal(c); err == nil {
		_ = os.MkdirAll(filepath.Dir(path), 0o755)
		_ = os.WriteFile(path, data, 0o644)
	}
}

// ---- loading ----

// loadSessions lists every transcript under dir, newest first. Transcripts
// without a cwd (never got past the first frame) cannot be resumed and are
// left out. cachePath "" disables the disk cache.
func loadSessions(dir, cachePath string) ([]*session, error) {
	if _, err := os.Stat(dir); err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("no transcripts at %s", tildePath(dir, homeDir()))
		}
		return nil, err
	}
	files, _ := filepath.Glob(filepath.Join(dir, "*", "*.jsonl"))

	var cache map[string]cacheEntry
	if cachePath != "" {
		cache = loadCache(cachePath)
	}
	fresh := make(map[string]cacheEntry, len(files))
	var mu sync.Mutex
	dirty := len(cache) != len(files)

	jobs := make(chan string)
	var wg sync.WaitGroup
	for w := 0; w < runtime.NumCPU(); w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for file := range jobs {
				st, err := os.Stat(file)
				if err != nil {
					continue
				}
				e, ok := cache[file]
				if !ok || e.MTime != st.ModTime().UnixNano() || e.Size != st.Size() {
					f, err := os.Open(file)
					if err != nil {
						continue
					}
					e = cacheEntry{MTime: st.ModTime().UnixNano(), Size: st.Size(), scanned: scanTranscript(f)}
					f.Close()
					mu.Lock()
					dirty = true
					mu.Unlock()
				}
				mu.Lock()
				fresh[file] = e
				mu.Unlock()
			}
		}()
	}
	for _, f := range files {
		jobs <- f
	}
	close(jobs)
	wg.Wait()

	if cachePath != "" && dirty {
		saveCache(cachePath, fresh)
	}

	sessions := make([]*session, 0, len(fresh))
	for file, e := range fresh {
		if e.Cwd == "" {
			continue
		}
		sessions = append(sessions, &session{
			id:      strings.TrimSuffix(filepath.Base(file), ".jsonl"),
			file:    file,
			cwd:     e.Cwd,
			branch:  e.Branch,
			title:   e.Title,
			prompt:  e.Prompt,
			last:    time.Unix(0, e.MTime),
			missing: !dirExists(e.Cwd),
		})
	}
	sort.Slice(sessions, func(i, j int) bool {
		if !sessions[i].last.Equal(sessions[j].last) {
			return sessions[i].last.After(sessions[j].last)
		}
		return sessions[i].id < sessions[j].id
	})
	return sessions, nil
}

// visibleSessions applies the two list modes: all also lists sessions whose
// directory is gone, here keeps the ones started in that directory or below.
func visibleSessions(sessions []*session, all bool, here string) []*session {
	out := make([]*session, 0, len(sessions))
	for _, s := range sessions {
		if s.missing && !all {
			continue
		}
		if here != "" && !insideDir(here, s.cwd) {
			continue
		}
		out = append(out, s)
	}
	return out
}

// markLive records the herdr pane each live session runs in.
func markLive(sessions []*session, live map[string]string) {
	for _, s := range sessions {
		s.pane = live[s.id]
	}
}

// ---- small helpers ----

func dirExists(p string) bool {
	st, err := os.Stat(p)
	return err == nil && st.IsDir()
}

func insideDir(dir, path string) bool {
	return path == dir || strings.HasPrefix(path, strings.TrimSuffix(dir, "/")+"/")
}

func tildePath(p, home string) string {
	if home == "" {
		return p
	}
	if p == home {
		return "~"
	}
	if strings.HasPrefix(p, home+"/") {
		return "~" + strings.TrimPrefix(p, home)
	}
	return p
}

func compactAge(t, now time.Time) string {
	s := int64(now.Sub(t).Seconds())
	if s < 0 {
		s = 0
	}
	switch {
	case s < 3600:
		return fmt.Sprintf("%dm", s/60)
	case s < 86400:
		return fmt.Sprintf("%dh", s/3600)
	case s < 604800:
		return fmt.Sprintf("%dd", s/86400)
	}
	return fmt.Sprintf("%dw", s/604800)
}

func countLabel(n int) string {
	if n == 1 {
		return "1 session"
	}
	return fmt.Sprintf("%d sessions", n)
}
