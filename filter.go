package main

// Fuzzy filtering. Rows keep their newest-first order while filtering (the
// list is a timeline, not a ranking); the score only decides where the cursor
// lands. Matched positions are byte offsets into the displayed string, as
// sahilm/fuzzy reports them.

import (
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/sahilm/fuzzy"
)

// sessionRow is one session in the list.
type sessionRow struct {
	s        *session
	score    int
	titleIdx []int // matched bytes in the title column
	pathIdx  []int // matched bytes in the ~ path
}

// filterSessions matches the query against each session's title, its ~ path
// and a hidden corpus (branch and id), keeping the best score per session.
func filterSessions(sessions []*session, q, home string) []sessionRow {
	rows := make([]sessionRow, 0, len(sessions))
	if q == "" {
		for _, s := range sessions {
			rows = append(rows, sessionRow{s: s})
		}
		return rows
	}
	titles := make([]string, len(sessions))
	paths := make([]string, len(sessions))
	hidden := make([]string, len(sessions))
	for i, s := range sessions {
		titles[i] = s.label()
		paths[i] = tildePath(s.cwd, home)
		hidden[i] = s.branch + " " + s.id
	}
	hits := map[int]*sessionRow{}
	hit := func(i, score int) *sessionRow {
		r, ok := hits[i]
		if !ok {
			r = &sessionRow{s: sessions[i], score: score}
			hits[i] = r
		} else if score > r.score {
			r.score = score
		}
		return r
	}
	for _, mt := range fuzzy.Find(q, titles) {
		hit(mt.Index, mt.Score).titleIdx = append([]int(nil), mt.MatchedIndexes...)
	}
	for _, mt := range fuzzy.Find(q, paths) {
		hit(mt.Index, mt.Score).pathIdx = append([]int(nil), mt.MatchedIndexes...)
	}
	for _, mt := range fuzzy.Find(q, hidden) {
		hit(mt.Index, mt.Score)
	}
	for i := range sessions {
		if r, ok := hits[i]; ok {
			rows = append(rows, *r)
		}
	}
	return rows
}

// bestIndex returns the position of the highest score; ties go to the first
// (newest) row. -1 for an empty list.
func bestIndex(n int, score func(int) int) int {
	best := -1
	for i := 0; i < n; i++ {
		if best == -1 || score(i) > score(best) {
			best = i
		}
	}
	return best
}

// highlight styles the matched bytes of s and applies base to the rest. A
// match keeps what base says (the selected row's background, a bold title)
// and adds the match color and the underline, as asgitlog does.
func highlight(s string, idx []int, base lipgloss.Style) string {
	if len(idx) == 0 {
		return base.Render(s)
	}
	set := make(map[int]bool, len(idx))
	for _, i := range idx {
		set[i] = true
	}
	var b, run strings.Builder
	flush := func() {
		if run.Len() > 0 {
			b.WriteString(base.Render(run.String()))
			run.Reset()
		}
	}
	for i, r := range s {
		if set[i] {
			flush()
			b.WriteString(matchOver(base).Render(string(r)))
		} else {
			run.WriteRune(r)
		}
	}
	flush()
	return b.String()
}

// matchOver is the match style on top of base.
func matchOver(base lipgloss.Style) lipgloss.Style {
	return base.Foreground(stMatch.GetForeground()).Underline(true)
}
