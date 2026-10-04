package main

// The sessions the popup lists: one <project>/<id>.jsonl per session under
// ~/.claude/projects. A transcript is scanned once (scanTranscript, in
// sessiontitle.go) for the directory it must be resumed from, its branch, its
// title and its first prompt; the result is cached on disk per (path, mtime,
// size) so reopening the popup only reads the transcripts that changed.

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"
)

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

type cacheEntry struct {
	MTime int64 `json:"mtime"`
	Size  int64 `json:"size"`
	scanned
}

func cacheFile() string {
	return filepath.Join(stateDir(), "sessions.json")
}

func loadCache(path string) map[string]cacheEntry {
	c := map[string]cacheEntry{}
	readJSONFile(path, &c)
	return c
}

func saveCache(path string, c map[string]cacheEntry) { writeJSONFile(path, c) }

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

func countLabel(n int) string {
	if n == 1 {
		return "1 session"
	}
	return fmt.Sprintf("%d sessions", n)
}

// stateDir is where asgotosession keeps its runtime state.
func stateDir() string { return stateDirFor("asgotosession") }
