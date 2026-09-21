# CLAUDE.md

Guidance for working in this repository.

## What this is

`asgotosession` is a herdr plugin popup that lists the user's Claude Code
sessions and resumes the chosen one in the herdr space of its directory. Open,
pick, exit: same lifecycle and look as `asgotonotes`, `asgotopr` and `asgoto`,
which this repo is modeled on. It started as the Go port of a bash + fzf
script; what it lists is the rule under Data source: every top-level
transcript that recorded a `cwd`, whose directory still exists unless the
scope includes the missing ones.

It only reads the transcripts. What it writes is its own: its settings (the
panel's option and the list size) and its cache. The only things it runs are
herdr CLI calls (or `claude --resume` outside herdr), after the TUI has quit.

Distributed as a herdr plugin (`herdr plugin install asumaran/asgotosession`;
the manifest's `[[build]]` runs `scripts/fetch-binary.sh`). Each GitHub Release
attaches the `asgotosession-<os>-<arch>` binaries (macOS and Linux, arm64 and amd64). There is no published library.

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

- `main.go`: flags (`-version`, `-dump`, `-all`, `-here`, `-query`, the
  internal `-await-focus`), `tea.NewProgram`, post-quit `runResume`, `runDump`
  (it writes to an `io.Writer`, so the tests read what `-dump` prints).
- `sessions.go`: pure logic: `scanTranscript` (raw-byte line matching),
  `loadSessions` (parallel scan + disk cache), `visibleSessions`, `markLive`,
  `stateDir()`.
- `herdr.go`: the herdr CLI behind a `runner` func (`herdrRun`, or `herdrAct`
  for `openInHerdr`, which may create a space; both of `herdrcli.go`, faked in
  tests):
  `liveSessions`, `openInHerdr`, `awaitAgentFocus` / `detachAgentFocus`,
  `runResume`.
- `filter.go`: fuzzy rows.
- `match.go`: `findTight`/`tighten`, the fuzzy matcher with one correction: it
  is greedy (first candidate for each rune, left to right), so a query that
  occurs in one piece could still match scattered letters before it. When the
  query occurs whole, that occurrence is the match, for the highlight and for
  the score. `hasTerms` says whether a query searches for anything: spaces and
  a bare `~` or `'` do not, so they never filter, rank or move the cursor. The
  same file in every tool of the family.
- `text.go`: `truncate`, `padRight`, `padLeft`: fitting text, styled or not,
  into cells. `errorBlock` is an error for a preview: every line of it cut to
  the width, in the error color. The same file in every tool of the family.
- `statedir.go`: `stateDirFor`: the state dir herdr injects
  (`HERDR_PLUGIN_STATE_DIR`) or, when the tool runs on its own, the same
  directory worked out
  (`${XDG_STATE_HOME:-~/.local/state}/herdr/plugins/asumaran.asgotosession`),
  so the popup and a run from the shell share settings and caches. The same
  file in every tool of the family.
- `setting.go`: `loadSetting`, `saveSetting`: a setting the tool remembers,
  one plain-text file each in the state dir. Every option of the panel is
  kept this way, per tool. The same file in every tool of the family that
  needs it.
- `age.go`: `compactAge` (`5m`, `3h`, `2d`, `6w`, `2y`) for a list column and
  `relTime` (`3h ago`) for a sentence. The same file in every tool of the
  family that shows an age.
- `listmouse.go`: `inList`, `rowUnder`, `wheelKey`: the mouse over the list.
  The wheel goes through the same code as the arrows; a click moves the
  cursor and never opens anything. The same file in every tool of the family.
- `prompt.go`: the filter input: its prompt (with the tool's name only outside
  herdr's popup), the placeholder, the `(dev)` mark on the edge over the
  input. `typeInto` hands a message to the input and reports whether the query
  changed: a key, a terminal paste and the input's own `ctrl+v` all edit it,
  and the caller filters again only when it did. The same file in every tool
  of the family.
- `helpfoot.go`: the help line at the foot, cut to the width, and the key that
  opens the panel. `footLine` is what the foot shows: a flash first, then a
  notice in the error color, else the help. The same file in every tool of the
  family.
- `panel.go`: the panel `f1` opens over the frame: options to change in
  place and every key under them (`option`, `panel`, `panelLines`,
  `overlay`). The same file in every tool of the family.
- `listnav.go`: `listNav`: the keys that move the cursor through a list and
  where each one takes it, group headers skipped. `scrollTo` keeps the cursor
  in view together with the row `withHeader` names: the header of its group
  when that is the row right above. `emptyList` is what a list says instead of
  rows: the error that kept it from loading, in the error color, `No matches`,
  or the tool's own reason. The same file in every tool of the family.
- `highlight.go`: `highlight`/`highlightFrom`, `matchOver`, `onSel`,
  `selPad` and the `stSel`/`stMatch` styles: how a match and the selected row
  look. The same file in every tool of the family.
- `flash.go`: `flash`, `flashMsg`, `flashErrMsg`, `clearFlashMsg`: a word that
  takes the help line for a moment: a confirmation in green (`flash.set`), or
  a key that could do nothing (`nothing to copy`) in the error color
  (`flash.fail`). The same file in every tool of the family.
- `clipboard.go`: `copyCmd`: feeds a text to the system clipboard and reports
  it with a `flashMsg`, or with a `flashErrMsg` when there is nothing to copy
  or the copy fails; `ASGOTOSESSION_CLIPBOARD` replaces the command. The same
  file in every tool of the family.
- `border.go`: `hline`, `framed`, `fit`, `scrollPos`: the primitives the frame
  is drawn with (an edge with texts set into it, a line between the frame's
  sides, the position a scrolled viewport reports on an edge). `fitLines` is
  content as exactly so many lines of a width, and `popupView` is the
  `tea.View` every tool returns: the alt screen and, while the mouse is on,
  cell-motion mouse reports. The same file in every tool of the family.
- `homepath.go`: `tildePath`, `homeDir`, `homeRel`: a path with the home
  directory abbreviated to `~`. The same file in every tool of the family that
  shows paths.
- `rank.go`: `rank`: with a query the list is a search result, best score
  first; in a grouped list the groups go by their best item and keep their
  items together, and equal scores keep the list's own order. The same file in
  every tool of the family that ranks its matches.
- `herdrbin.go`: `herdrBin`: where the herdr executable is (`HERDR_BIN_PATH`,
  which the server hands to plugin commands, else `herdr` on `PATH`). The same
  file in every tool of the family that talks to herdr.
- `herdrcli.go`: `herdrRun`, `herdrAct`, `herdrDo`, `herdrError`: running the herdr CLI. A
  read (`herdrRun`) gets 5 seconds and a command that changes something
  (`herdrAct`, `herdrDo`) 30, so a server that does not answer is an error, never a hang,
  and a failure is said the way herdr said it (the message of its JSON error
  object). The same file in every tool of the family that runs herdr.
- `panecwd.go`: `paneDirs`, `paneCwd`, `enterPaneCwd`: which directory a popup
  was opened from. herdr starts a plugin pane in the plugin's own directory
  and hands over `focused_pane_cwd` and `workspace_cwd` in
  `HERDR_PLUGIN_CONTEXT_JSON`; a plain run uses the working directory. The
  same file in every tool of the family that needs it.
- `pathcells.go`: `pathCells`, `pathTail`, `tailCut`: a path cut to a width
  by its head, its prefix dimmed, its matches marked. The same file in every
  tool that lists paths.
- `opener.go`: `openerArgv`: the command `<TOOL>_OPENER` names, as words, or
  nothing when the variable is unset and the tool's own default applies. The
  value is a command line, not a path: `code -n` and a wrapper with flags both
  work, a path with spaces does not. The same file in every tool of the family
  that opens something.
- `jsonfile.go`: `readJSONFile`, `writeJSONFile`, `writeFileAtomic`: a JSON
  cache in the state dir. A file that is missing or does not parse reads as
  nothing, and a write goes through a temporary file and a rename, so a popup
  closed mid-write, or two of them writing at once, never leave half a file
  for the next run. The same file in every tool of the family that keeps one.
- `frame.go`: the single-frame layout the pickers share: `frameHead`,
  `splitMain` (list and preview) and the section rows (`mainY`, `listY`,
  `frameRows`, each with or without the optional context line), drawn with the
  primitives of `border.go`. Copied, not imported: the same file ships in
  asgoto, asgotopr, asgotoissues, asgotonotes and asgotochanged (all under
  github.com/asumaran), and there is no shared library. A pull request only
  needs to change it here; the maintainer ports the change to the other
  copies.
- `split.go`: the divider between the list and the preview: `loadSplit`,
  `saveSplit`, `stepSplit`, `splitWidths`, `moveSplit` (one step, remembered)
  and `sizePanes` (the list and the preview get their share of the main
  section). Copied, not imported, like `frame.go`: the same file ships in
  asgotopr, asgotoissues, asgotonotes and asgotochanged.
- `ui.go`: the bubbletea model/Update/View, toggles, mouse, styles.
- `preview.go`: session preview as a `tea.Cmd` (tail of the transcript,
  prompts and replies only), render cache, and the instant header
  (`previewHeader`: title, directory, branch and id, live pane) that stays
  fixed above the conversation with one blank line under it.
- `scripts/pty-check.py`: end-to-end TUI driver (see Testing).

## Build & run

```bash
go build -o asgotosession .    # plugin runs ./asgotosession from the repo root
./asgotosession -dump          # sessions as the popup would list them, no TTY
./asgotosession -dump -all     # include sessions whose directory is gone
./asgotosession -dump -query x # matches with scores
go vet ./... && go test ./...
scripts/pty-check.py ./asgotosession   # end-to-end TUI check on a pty (python3 + pyte)
herdr plugin link "$PWD"   # link does NOT run [[build]]; go build yourself
```

Keybinding (user config): `prefix+y` / `ctrl+alt+h` → `plugin_action`
`asumaran.asgotosession.open` → `scripts/open-pane.sh` → `herdr plugin pane open`.

## Behaviour / decisions

- **Layout**: one rounded frame of sections split by shared edges, the layout
  asgitlog introduced and every picker of the family follows (`frame.go`, the
  same file in each repo): the filter input (the border over it carries, in brackets, what the list is
  narrowed or widened to, like asgitlog's scope), the main section (list and
  preview split by a divider; its bottom edge carries the matches/total
  counter under the list and, while the preview overflows, its scroll position
  on the right),
  and the help. A context line on top is only for what the rest of the screen
  cannot say (asgitlog: repo and branch); a title is not context, so there is
  none here. The list starts on screen row `listY`, one cell in from the left
  side, which is what the click-to-row math uses. Errors and notices take the
  help line.
- **Moving through the list** is the same in every tool of the family and
  comes from `listnav.go` (the same file in each repo): arrows or
  `ctrl+p`/`ctrl+n` a row, `pgup`/`pgdn` a page, `alt+↑`/`alt+↓` or
  `home`/`end` the ends. `home`/`end` are taken from the filter input's caret
  on purpose (`←`/`→` and `ctrl+e` still move it). The preview scrolls with
  `shift+↑`/`shift+↓` only. The keys are listed in the panel.
- **The filter input** comes from `prompt.go` (the same file in every tool of
  the family). Inside herdr's popup the prompt is the arrow alone, because the
  pane's title (`[[panes]] title` in the manifest, the tool's name) already
  says which tool it is, and a placeholder says what the filter searches. Run
  on its own the prompt carries the tool's name. A build that is not a release
  says `(dev)` on the edge over the input, never inside the
  prompt.
  herdr sets `HERDR_PLUGIN_ENTRYPOINT_ID` for a plugin pane; that is how the
  two cases are told apart.
  Whatever reaches the input goes through `toInput`: a key, a paste from the
  terminal (`tea.PasteMsg`) and the input's own `ctrl+v` filter the list the
  same way (`typeInto`), and a message that leaves the query alone (a caret
  move, the blink) never moves the cursor. A paste under the open panel is
  dropped. A query made only of spaces, or a bare `~` or `'`, is not a query
  (`hasTerms`): it does not filter, rank or move the cursor.
- **Help and options**: the line at the foot shows the tool's own actions,
  the panel's key and the quit keys (`helpfoot.go`). `f1` opens the panel (`panel.go`, the same file in
  every tool of the family): the options on top, to change with `←`/`→` or
  `space`, and every key in columns under them, laid out by bubbles' `help`
  from `FullHelp()`. The panel is spliced over the middle of the frame, which
  keeps its size; while it is open it takes every key and the mouse, and `esc`
  closes it before it does anything else. `?` is not a help key: the filter
  has the focus, so it is text. Moving, scrolling and resizing are listed in
  the panel only, so the help line stays short enough for a narrow popup. A
  message takes the help line's place (`footLine`): a flash for a moment (a
  confirmation in green, a key that could do nothing in the error color),
  else an error or a notice in the error color.
  This tool's options are the scope `ctrl+a` walks: `options()` lists them as things stand and
  `setOption` is the one place that changes a setting, for the panel and for
  the keys that kept a shortcut. A setting that is chosen once has no key of
  its own; the panel is where it lives.
- **Filter matches** look the same in every tool of the family and come from
  one place, `highlight.go` (the same file in each repo; it also owns `stSel`
  and `stMatch`): a match is the match color plus an underline on top of the
  style the text already has, and the selected row shows them too. That row
  is never one big `stSel.Render` around styled text, because the reset that
  ends a match would cut the background: every piece is rendered over `stSel`
  (`highlight(s, idx, stSel)`) and `selPad` fills the rest. Do not write a
  local highlighter.
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
  `~/.local/state/herdr/plugins/asumaran.asgotosession`), keyed by path and valid while mtime and
  size match. Entries of deleted transcripts are dropped on the next save.
  It is read and written through the shared `jsonfile.go`.
  Whether the directory still exists is NOT cached: it is checked every run.
- **A query makes the list a search result**: rows are ranked, best match
  first, and the cursor sits on the first one (`rank` in `rank.go`, the same
  file in every picker of the family). Equal scores keep the list's own
  order, newest first, which is also the order without a query. A score says how
  good the match is and nothing about the length of the text (`match.go`).
  It matches the title column, the `~`
  path and a hidden corpus (branch + id). Matched indexes from `sahilm/fuzzy`
  are BYTE offsets. With an empty query the cursor stays on the row it was on
  (`refilter`).
- **Settings**: the scope is one file in the state dir (`scope`;
  `setting.go`, the same file in every tool of the family that remembers an
  option), next to the divider's `split-columns`.
- **Modes**: `ctrl+a`, the family's "list more" key, walks the scopes
  (`nextScope`): the directory the popup was opened from (`-here`),
  everywhere, everywhere plus the sessions whose directory is gone (`-all`),
  and around again. `tab` is left for marking rows, as in asgotonotes. In a
  plugin pane that directory is `focused_pane_cwd` from
  `HERDR_PLUGIN_CONTEXT_JSON` (herdr starts plugin panes in the plugin's own
  directory); in a plain run it is the cwd. Without one the cycle leaves
  that step out. The scope is remembered (setting `scope`); `-here` and
  `-all` go before it. The toggle's help text names the scope it moves to next.
- **Keys vs. filter**: every printable key filters, so `q` quits only while
  the filter is empty. `ctrl+a` is intercepted before the textinput (which
  would treat it as line-start).
- **Copy**: `ctrl+y` copies the id of the session under the cursor (what
  `claude --resume` takes) with `copyCmd` (`clipboard.go`) and the help line
  flashes `copied <id>` in green, or `nothing to copy` and `copy failed: ...`
  in the error color (`flash.go`, shown before the notice); both files are
  the same in every tool of the family. `ASGOTOSESSION_CLIPBOARD` replaces
  the clipboard command, which is how the tests and the pty check log it.
- **Paths** that do not fit lose their head, not their tail (`pathCells`), so
  the worktree name is always visible.
- **Resuming** happens after the TUI quits (quitting closes the popup and
  anything printed after that is lost). Order in `openInHerdr`: pane where the
  session is live (`agent_session.value`) → idle shell in the directory's
  space (`foreground_process_group_id == shell_pid`) → new tab in that space →
  new space. The space is found by `worktree.checkout_path`, else by any pane
  sitting in that directory. A missing directory never quits: the popup stays
  open with a notice on the help line, in the error color, until the next key
  (`footLine`).
- **Errors**: transcripts that cannot be listed are reported in the list in
  the error color (`loadErr` through the shared `emptyList`), as in every
  tool of the family; the popup still opens.
- **Agent focus is detached**: herdr needs a moment to recognize claude in the
  pane, and polling for it in-process would leave an empty popup on screen.
  `detachAgentFocus` re-runs the binary with `-await-focus <pane>` in its own
  session (`Setsid`) and returns.
- **Outside herdr** (`HERDR_ENV` != 1) the process `chdir`s and `exec`s
  `claude --resume <id>`; `ASGOTOSESSION_OPENER` replaces the command words
  (`opener.go`).
- **Preview** reads at most the last 2 MB, keeps the last 40 turns clipped to
  1200 runes each, skips tool calls, tool results, tagged harness entries and
  sidechains, and lands scrolled to the bottom. Cached per (file, width,
  mtime). A transcript that cannot be read shows the error through the
  shared `errorBlock` (`text.go`). There is no markdown rendering, so the
  program never needs the terminal background color.
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
./asgotosession` (python3 + `pyte`) spawns the binary on a pty, answers the
terminal queries, replays keystrokes and asserts on pyte-rendered frames. It
runs in a throwaway sandbox (fake `HOME`, synthetic transcripts via
`CLAUDE_PROJECTS_DIR`, logging stubs as `ASGOTOSESSION_OPENER`,
`HERDR_BIN_PATH` and `ASGOTOSESSION_CLIPBOARD`), so it never reads the real transcripts, never talks to a
herdr server and never starts claude. Fixture transcripts must be compact
JSON, like the real ones.

## Commits & branches

- Conventional Commits: `type(scope): description` (feat, fix, chore, docs,
  style, refactor, test, perf).
- Never mention AI tooling in commits, PRs, or any repo-visible text as the
  author of changes.
- Default branch is `main`. Don't commit, tag, or push unless explicitly
  asked (releasing is an explicit, separate request).
- `HANDOFF.md` and `REPORT.md` at the root are local notes and are gitignored.

## Releasing

`scripts/release.sh <X.Y.Z>`: clean-tree + vet/build/test gate, CHANGELOG
generation from commit subjects, manifest version sync, commit + tag + GitHub
release; CI (`.github/workflows/release.yml`) attaches
the `asgotosession-<os>-<arch>` binaries (macOS and Linux, arm64 and amd64). Releasing never touches the linked plugin's
`./asgotosession`; rebuild locally to keep testing dev code.
