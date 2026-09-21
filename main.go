// asgotosession: a herdr plugin popup that lists your Claude Code sessions and
// resumes the chosen one in the herdr space of its directory: the pane where
// it is already live, an idle shell of that space, a new tab in it, or a new
// space. Outside herdr it resumes in place, as a plain command.
//
// The data comes from the transcripts under ~/.claude/projects. asgotosession
// only reads them.
package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"time"

	tea "charm.land/bubbletea/v2"
)

// version is the release tag; overridden at build time via
// -ldflags "-X main.version=vX.Y.Z" (see scripts/release.sh and CI).
var version = "dev"

func main() {
	showVersion := flag.Bool("version", false, "print the embedded version")
	dump := flag.Bool("dump", false, "print the sessions (no TUI)")
	all := flag.Bool("all", false, "also list sessions whose directory no longer exists")
	here := flag.Bool("here", false, "only sessions started in the current directory (or below it)")
	query := flag.String("query", "", "initial filter; with -dump, print the matches and their scores")
	awaitFocus := flag.String("await-focus", "", "internal: focus this pane once herdr sees the agent in it")
	flag.Usage = func() {
		fmt.Fprintln(flag.CommandLine.Output(), "usage: asgotosession [flags] [query]")
		flag.PrintDefaults()
	}
	flag.Parse()

	if *showVersion {
		fmt.Println(version)
		return
	}
	if *awaitFocus != "" {
		awaitAgentFocus(herdrRun, *awaitFocus)
		return
	}
	if *query == "" && flag.NArg() > 0 {
		*query = flag.Arg(0)
	}

	start := time.Now()
	sessions, err := loadSessions(projectsDir(), cacheFile())
	loadErr := ""
	if err != nil {
		loadErr = err.Error()
	}
	if inHerdr() {
		markLive(sessions, liveSessions(herdrRun))
	}
	opts := options{all: *all, here: *here, dir: paneCwd(), query: *query}

	if *dump {
		if err != nil {
			fmt.Fprintln(os.Stderr, "asgotosession:", err)
			os.Exit(1)
		}
		runDump(os.Stdout, sessions, opts, time.Since(start))
		return
	}

	// Alt screen and mouse mode are declared per frame by View().
	res, err := tea.NewProgram(newModel(sessions, loadErr, opts)).Run()
	if err != nil {
		fmt.Fprintln(os.Stderr, "asgotosession:", err)
		os.Exit(1)
	}
	if s := res.(model).chosen; s != nil {
		if err := runResume(s); err != nil {
			fmt.Fprintln(os.Stderr, "asgotosession:", err)
			os.Exit(1)
		}
	}
}

// runDump prints what the popup would list, without a TTY. With -query it
// prints the matches and their scores instead.
func runDump(w io.Writer, sessions []*session, opts options, took time.Duration) {
	home, now := homeDir(), time.Now()
	opts = opts.resolved()
	here := ""
	if opts.here {
		here = opts.dir
	}
	visible := visibleSessions(sessions, opts.all, here)
	fmt.Fprintf(w, "transcripts: %s, %d resumable, %d shown (scope: %s), loaded in %s\n",
		tildePath(projectsDir(), home), len(sessions), len(visible), opts.scopeName(), took.Round(time.Millisecond))

	if opts.query != "" {
		fmt.Fprintf(w, "query %q:\n", opts.query)
		for _, r := range filterSessions(visible, opts.query, home) {
			fmt.Fprintf(w, "  %5d  %-40s %s\n", r.score, truncate(r.s.label(), 40), tildePath(r.s.cwd, home))
		}
		return
	}

	for _, s := range visible {
		state := " "
		switch {
		case s.pane != "":
			state = "●"
		case s.missing:
			state = "x"
		}
		fmt.Fprintf(w, "%s %4s  %s  %-50s %s\n", state, compactAge(s.last, now), s.id,
			truncate(s.label(), 50), tildePath(s.cwd, home))
	}
}
