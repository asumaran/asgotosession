# asgotosession

A [herdr](https://github.com/asumaran/herdr) plugin popup that lists your
Claude Code sessions and resumes the one you pick in the herdr space of its
directory. It also runs as a plain command in any shell, where it resumes the
session in place.

Sibling of [asgotonotes](https://github.com/asumaran/asgotonotes),
[asgotopr](https://github.com/asumaran/asgotopr) and
[asgoto](https://github.com/asumaran/asgoto): same open-pick-exit
popup, same fuzzy search.

```
╭─────────────────────────────────────────────────────────────────────────────────────────────────── 358/358 ─╮
│ asgotosession ❯                                                                                             │
├────────────────────────────────────────────────────────────┬────────────────────────────────────────────────┤
│▌●Checkout form validation  …fix-checkout-form-validation 17m│ ❯ now the error messages                      │
│ ●Release plan               ~/wt/shop/chore-release-plan 46m│                                               │
│  Remove the old fzf helpers ~/Developer/dotfiles          1h│ Done: each field reports its own              │
│  Cache layer rewrite        ~/Developer/tool              4h│ error under the input. …                      │
├────────────────────────────────────────────────────────────┴──────────────────────────────────────── 12/12 ─┤
│ type filter • enter resume • tab this dir • ^a missing dirs • ⇧↓ scroll preview • ⇧←/⇧→ resize • esc/q quit │
╰─────────────────────────────────────────────────────────────────────────────────────────────────────────────╯
```

## Requirements

Claude Code (the `claude` CLI), macOS or Linux and, for the popup, herdr >= 0.7.5.
The sessions are read from the transcripts Claude Code already keeps under
`~/.claude/projects`; there is nothing to set up. `CLAUDE_PROJECTS_DIR` points
it at another directory.

## Install

```
herdr plugin install asumaran/asgotosession
```

The manifest's `[[build]]` runs `scripts/fetch-binary.sh`, which downloads the
release binary matching the manifest version and falls back to `go build`
(`ASGOTOSESSION_BUILD_FROM_SOURCE=1` skips the download).

Bind a key to the `open` action in `~/.config/herdr/config.toml`:

```toml
[[keys.command]]
key = ["prefix+y", "ctrl+alt+h"]
type = "plugin_action"
command = "asumaran.asgotosession.open"
description = "asgotosession (Claude session switcher)"
```

To use it outside herdr, put the binary on your `PATH`:

```
ln -s "$(herdr plugin list --json | jq -r '.result.plugins[] | select(.plugin_id == "asumaran.asgotosession") | .plugin_root')/asgotosession" ~/.local/bin/asgotosession
```

## Usage

The filter input is focused on open, so just type. One row per session,
newest activity first: title, directory and age. The title is the one you gave
the session, otherwise the one Claude generated, otherwise the first prompt
you typed. A `●` marks the sessions that are running in some herdr pane right
now. The filter matches the title, the directory, the branch and the session
id.

The frame's top border shows how many sessions match out of the total and, in
brackets, what the list is narrowed or widened to (`[in ~/some/dir]`,
`[+missing dirs]`).

The right side shows the session under the cursor: directory, branch, id and
the end of the conversation, your prompts and Claude's replies only. It opens
scrolled to the bottom, since the last exchange is usually how you recognize a
session.

| key | action |
| --- | --- |
| `enter` | resume the session |
| `tab` | only the sessions started in the directory the popup was opened from (or below it), and back |
| `ctrl+a` | also list sessions whose directory no longer exists, and back |
| `↑/↓`, `ctrl+p`/`ctrl+n` | move the cursor |
| `shift+↓`/`shift+↑`, PgDn/PgUp, mouse wheel over the preview | scroll the preview |
| mouse wheel over the list | move the cursor |
| `shift+←`/`shift+→` | resize the list; the split is remembered (the list takes a quarter of the width by default) |
| click | select a row |
| `esc`, `q` with an empty filter | close |

### Where a session is resumed

Inside herdr, `enter` picks the first of these that applies:

1. The session is live in some pane: that pane takes the focus and nothing
   new is started.
2. The directory has a space and an idle shell sits in it, in that directory:
   `claude --resume <id>` runs there.
3. The directory has a space: a new tab labelled `claude` opens in it.
4. Otherwise a new space is created for the directory.

Outside herdr the process changes to the session's directory and becomes
`claude --resume <id>`.

A session whose directory is gone cannot be resumed (Claude Code resumes from
the directory the session started in); the popup stays up and says so.

### As a command

```
asgotosession [-here] [-all] [query]
```

`-here` starts narrowed to the current directory, `-all` starts with the
missing directories listed, and `query` is the initial filter.
`CLAUDE_SESSIONS_CMD` replaces `claude` (a wrapper, or extra flags).

## Behavior notes

- asgotosession only reads the transcripts. The one thing it writes is its own
  cache.
- A transcript is scanned once for its directory, branch, title and first
  prompt, and the result is cached per (path, mtime, size) in the plugin state
  dir. About 550 transcripts (550 MB) list in half a second the first time and
  in 10 ms after that.
- The preview reads only the last 2 MB of a transcript, off the UI loop.
- The directory of a session is the first one it recorded, the one Claude
  Code resumes from, even if the session moved around later.
- After starting `claude` in a pane, a detached helper waits for herdr to
  recognize the agent and focuses it, so the popup closes right away.

## Development

```bash
go build -o asgotosession .   # local build (plugin runs ./asgotosession from the repo root)
./asgotosession -dump         # print the sessions as the popup would list them (no TTY)
./asgotosession -dump -all    # include the ones whose directory is gone
./asgotosession -dump -query eshop   # matches with their scores
go vet ./... && go test ./...
scripts/pty-check.py ./asgotosession   # end-to-end TUI check on a pty (python3 + pyte)
herdr plugin link ~/Developer/asgotosession   # register the working copy (no build step)
```

`ASGOTOSESSION_POPUP_WIDTH` / `ASGOTOSESSION_POPUP_HEIGHT` override the popup size
from the manifest.

## Releasing

`scripts/release.sh <X.Y.Z>` gates on a clean tree + green vet/build/test,
generates the CHANGELOG entry from commit subjects, syncs the manifest
version, commits, tags and publishes the GitHub release; CI then attaches
the `asgotosession-<os>-<arch>` binaries (macOS and Linux, arm64 and amd64), the assets `fetch-binary.sh` downloads on installs.
