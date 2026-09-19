# CLAUDE.md

Guidance for working in this repository.

## What this is

`gotosession` is a herdr plugin popup that lists the user's Claude Code
sessions and resumes the chosen one in the herdr space of its directory. Open,
pick, exit: same lifecycle and look as `gotonotes`, `gotopr` and `herdr-goto`,
which this repo is modeled on. It is the Go port of `fcs`, a bash + fzf
script, and must keep listing exactly what that script listed.

It only reads the transcripts. It writes nothing except its own cache, and the
only things it runs are herdr CLI calls (or `claude --resume` outside herdr),
after the TUI has quit.

Distributed as a herdr plugin (`herdr plugin install asumaran/gotosession`;
the manifest's `[[build]]` runs `scripts/fetch-binary.sh`). Each GitHub Release
attaches `gotosession-darwin-arm64`. There is no published library.

## Data source

`~/.claude/projects/<project>/<session id>.jsonl` (override:
`CLAUDE_PROJECTS_DIR`), written by Claude Code, one JSON object per line,
compact (no space after `:`). Only files at that depth are sessions; deeper
ones are subagent transcripts. What is read from each:

- `cwd`: the FIRST `"cwd":"…"` in the file, the directory Claude Code resumes
  from. A transcript without one never got past its first frame and is left
  out. `gitBranch` comes from the same line.
- Title: a `{"type":"custom-title"` line always wins; otherwise the LAST
  `{"type":"ai-title"` line.
- First prompt: the first `user` message, not a sidechain, whose text does not
  start with `<` (harness entries such as command output, reminders and
  caveats are wrapped in tags). Shown when there is no title.
- Last activity: the file mtime.

The format is owned by Claude Code: do not assume fields beyond these.

## Stack & layout

Go single module, single `package main`, static binary. TUI: Bubble Tea v2 +
bubbles v2 (`textinput`, `viewport`, `key`, `help`), lipgloss v2,
`sahilm/fuzzy` for matching. The charm v2 modules are imported under their
canonical `charm.land/<name>/v2` paths (the
`github.com/charmbracelet/<name>/v2` spelling is rejected by `go get`). Files
are split by concern:

- `main.go` — flags (`-version`, `-dump`, `-all`, `-here`, `-query`, the
  internal `-await-focus`), `paneCwd`, `tea.NewProgram`, post-quit
  `runResume`, `runDump`.
- `sessions.go` — pure logic: `scanTranscript` (raw-byte line matching),
  `loadSessions` (parallel scan + disk cache), `visibleSessions`, `markLive`,
  `compactAge`, `tildePath`.
- `herdr.go` — the herdr CLI behind a `runner` func (faked in tests):
  `liveSessions`, `openInHerdr`, `awaitAgentFocus` / `detachAgentFocus`,
  `runResume`.
- `filter.go` — fuzzy rows, `bestIndex`, `highlight`.
- `frame.go` — the single-frame layout shared by the family: `hline`, `fit`,
  `framed`, `frameHead`, `splitMain`, `scrollPos` and the section rows (`mainY`,
  `listY`, `frameRows`, each with or without the optional context line).
- `split.go` — the divider between the list and the preview: `loadSplit`,
  `saveSplit`, `stepSplit`, `splitWidths`. The file is copied, not imported:
  the same one ships in gotochanged, gotonotes, gotopr and gotojira (all under
  github.com/asumaran), and there is no shared library. A pull request only
  needs to change it here; the maintainer ports the change to the other copies.
- `ui.go` — the bubbletea model/Update/View, toggles, mouse, styles,
  `pathCells`.
- `preview.go` — session preview as a `tea.Cmd` (tail of the transcript,
  prompts and replies only), render cache.
- `scripts/pty-check.py` — end-to-end TUI driver (see Testing).

## Build & run

```bash
go build -o gotosession .    # plugin runs ./gotosession from the repo root
./gotosession -dump          # sessions as the popup would list them, no TTY
./gotosession -dump -all     # include sessions whose directory is gone
./gotosession -dump -query x # matches with scores
go vet ./... && go test ./...
herdr plugin link ~/Developer/gotosession   # link does NOT run [[build]]; go build yourself
```

Keybinding (user config): `prefix+y` / `ctrl+alt+h` → `plugin_action`
`asumaran.gotosession.open` → `scripts/open-pane.sh` → `herdr plugin pane open`.

## Behaviour / decisions

- **Layout**: one rounded frame of sections split by shared edges, the layout
  asgitlog introduced and every picker of the family follows (`frame.go`, the
  same file in each repo): the filter input (the border over it carries the
  matches/total counter and, in brackets, what the list is narrowed or widened
  to, like asgitlog's scope), the main section (list and preview split by a
  divider; its bottom edge carries the preview's scroll position), and the
  help. A context line on top is only for what the rest of the screen cannot
  say (asgitlog: repo and branch); a title is not context, so there is none
  here. The list starts on screen row `listY`, one cell in from the left side,
  which is what the click-to-row math uses. Errors and notices take the help
  line.
- **Resizable list**: `shift+←/→` move the divider in 5% steps, as in
  asgitlog. The setting is the PREVIEW's share of the width, clamped to
  30-85 and saved as `split-columns` in the state dir; the default is 75
  (list 25%, preview 75%), the same in every picker of the family. Rows
  must degrade for a narrow list instead of truncating their last columns.
  A list too narrow for a readable title (under 24 cells) drops the directory
  column, which the preview shows anyway (`sessionLine`).
- **Scanning matches raw bytes**, decoding only title lines, the first cwd
  line and candidate prompt lines. Transcripts run to tens of megabytes and
  almost every line is a message body; decoding them all is what made the bash
  version need `rg`. A key inside a message body is escaped (`\"cwd\":`), so
  the raw match only ever hits real keys.
- **Disk cache**: `sessions.json` in `HERDR_PLUGIN_STATE_DIR` (standalone:
  `~/.config/herdr/gotosession-tui`), keyed by path and valid while mtime and
  size match. Entries of deleted transcripts are dropped on the next save.
  Whether the directory still exists is NOT cached: it is checked every run.
- **Filtering keeps the newest-first order**; the fuzzy score only decides
  where the cursor lands (`bestIndex`). It matches the title column, the `~`
  path and a hidden corpus (branch + id). Matched indexes from `sahilm/fuzzy`
  are BYTE offsets. With an empty query the cursor stays on the row it was on
  (`refilter`).
- **Modes**: `ctrl+a` adds the sessions whose directory is gone (`-all`);
  `tab` narrows to the directory the popup was opened from (`-here`). In a
  plugin pane that directory is `focused_pane_cwd` from
  `HERDR_PLUGIN_CONTEXT_JSON` (herdr starts plugin panes in the plugin's own
  directory); in a plain run it is the cwd. `tab` is disabled when there is
  none. Each toggle's help text describes what pressing it does next.
- **Keys vs. filter**: every printable key filters, so `q` quits only while
  the filter is empty. `ctrl+a` is intercepted before the textinput (which
  would treat it as line-start).
- **Paths** that do not fit lose their head, not their tail (`pathCells`), so
  the worktree name is always visible.
- **Resuming** happens after the TUI quits (quitting closes the popup and
  anything printed after that is lost). Order in `openInHerdr`: pane where the
  session is live (`agent_session.value`) → idle shell in the directory's
  space (`foreground_process_group_id == shell_pid`) → new tab in that space →
  new space. The space is found by `worktree.checkout_path`, else by any pane
  sitting in that directory. A missing directory never quits: the popup stays
  open with a footer notice.
- **Agent focus is detached**: herdr needs a moment to recognize claude in the
  pane, and polling for it in-process would leave an empty popup on screen.
  `detachAgentFocus` re-runs the binary with `-await-focus <pane>` in its own
  session (`Setsid`) and returns.
- **Outside herdr** (`HERDR_ENV` != 1) the process `chdir`s and `exec`s
  `claude --resume <id>`; `CLAUDE_SESSIONS_CMD` replaces the command words.
- **Preview** reads at most the last 2 MB, keeps the last 40 turns clipped to
  1200 runes each, skips tool calls, tool results, tagged harness entries and
  sidechains, and lands scrolled to the bottom. Cached per (file, width,
  mtime). There is no markdown rendering, so the program never needs the
  terminal background color.
- **Mouse**: the wheel follows the pointer, as in asgitlog: over the list
  (`overList`) it moves the cursor through the same code as the arrow keys,
  anywhere else it scrolls the preview. A left click on a list row moves the
  cursor and never resumes anything.
- **Alt screen and mouse mode** are declared per frame in `View()`; there is
  no `tea.WithAltScreen` program option in v2.

## Testing

Unit tests cover the pure logic with fixtures: transcript scanning, the disk
cache, the list modes, filtering, the four ways of resuming (through a fake
`runner`), preview rendering and key handling.

For end-to-end verification without a TTY, `scripts/pty-check.py
./gotosession` (python3 + `pyte`) spawns the binary on a pty, answers the
terminal queries, replays keystrokes and asserts on pyte-rendered frames. It
runs in a throwaway sandbox (fake `HOME`, synthetic transcripts via
`CLAUDE_PROJECTS_DIR`, logging stubs as `CLAUDE_SESSIONS_CMD` and
`HERDR_BIN_PATH`), so it never reads the real transcripts, never talks to a
herdr server and never starts claude. Fixture transcripts must be compact
JSON, like the real ones.

## Commits & branches

- Conventional Commits: `type(scope): description`.
- Never mention AI tooling in commits, PRs, or any repo-visible text as the
  author of changes.
- Default branch is `main`. Don't commit, tag, or push unless explicitly
  asked (releasing is an explicit, separate request).
- `HANDOFF.md` and `REPORT.md` at the root are local notes and are gitignored.

## Releasing

`scripts/release.sh <X.Y.Z>` — clean-tree + vet/build/test gate, CHANGELOG
generation from commit subjects, manifest version sync, commit + tag + GitHub
release; CI (`.github/workflows/release.yml`) attaches
`gotosession-darwin-arm64`. Releasing never touches the linked plugin's
`./gotosession`; rebuild locally to keep testing dev code.
