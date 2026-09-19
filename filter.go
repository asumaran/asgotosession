package main

// Fuzzy filtering. Rows keep their newest-first order while filtering (the
// list is a timeline, not a ranking); the score only decides where the cursor
// lands. Matched positions are byte offsets into the displayed string, as
// sahilm/fuzzy reports them.

import ()

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
	for _, mt := range findTight(q, titles) {
		hit(mt.Index, mt.Score).titleIdx = append([]int(nil), mt.MatchedIndexes...)
	}
	for _, mt := range findTight(q, paths) {
		hit(mt.Index, mt.Score).pathIdx = append([]int(nil), mt.MatchedIndexes...)
	}
	for _, mt := range findTight(q, hidden) {
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
