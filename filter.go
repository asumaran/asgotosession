package main

// Fuzzy filtering. Without a query the rows keep their newest-first order; with
// one the list is a search result, best match first (rank.go), and the cursor
// starts on it. Matched positions are byte offsets into the displayed text, as
// match.go reports them.

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
	hits := findFields(q, titles, paths, hidden)
	for i := range sessions {
		if h, ok := hits[i]; ok {
			rows = append(rows, sessionRow{s: sessions[i], score: h.Score, titleIdx: h.Any[0], pathIdx: h.Any[1]})
		}
	}
	return rank(rows, func(r sessionRow) int { return r.score }, nil) // a search result: best match first
}
