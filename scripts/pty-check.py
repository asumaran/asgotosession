#!/usr/bin/env python3
"""End-to-end TUI check for asgotosession without a real terminal.

Spawns the binary on a pty, answers the terminal queries bubbletea sends
(OSC 10/11, CSI 6n, DA1), replays keystrokes, and asserts on frames rendered
with pyte. Everything runs in a throwaway sandbox: a fake HOME, synthetic
transcripts (CLAUDE_PROJECTS_DIR), a logging stub instead of claude
(CLAUDE_SESSIONS_CMD) and a logging stub instead of herdr (HERDR_BIN_PATH).
It never reads the real transcripts, never talks to a herdr server and never
starts claude.

Usage: scripts/pty-check.py ./asgotosession   (needs python3 + pyte)
"""
import atexit, fcntl, json, os, pty, select, shutil, signal, struct, subprocess, sys, tempfile, termios, time, re
import pyte

BIN = os.path.abspath(sys.argv[1])
ROWS, COLS = 16, 150
SANDBOX = os.path.realpath(tempfile.mkdtemp(prefix="asgotosession-pty-"))

# ---------- sandbox: fake home, transcripts, stubs ----------
home = os.path.join(SANDBOX, "home")
projects = os.path.join(home, ".claude", "projects")
wt = os.path.join(home, "wt", "shop", "fix-ESHOP-551-structured-data")
tool = os.path.join(home, "Developer", "tool")
removed = os.path.join(home, "wt", "shop", "removed-branch")
for d in (wt, tool):
    os.makedirs(d)

def write(path, text):
    os.makedirs(os.path.dirname(path), exist_ok=True)
    with open(path, "w") as f:
        f.write(text)
    return path

# Claude Code writes compact JSON, and the binary matches keys on raw bytes.
def compact(obj):
    return json.dumps(obj, separators=(",", ":"))

def transcript(project, sid, cwd, lines, age):
    head = {"type": "user", "cwd": cwd, "gitBranch": "main", "isSidechain": False,
            "message": {"role": "user", "content": lines[0]}}
    body = [compact(head)]
    for i, text in enumerate(lines[1:]):
        if i % 2 == 0:
            body.append(compact({"type": "assistant", "message": {"content": [{"type": "text", "text": text}]}}))
        else:
            body.append(compact({"type": "user", "message": {"content": text}}))
    path = write(os.path.join(projects, project, sid + ".jsonl"), "\n".join(body) + "\n")
    t = time.time() - age
    os.utime(path, (t, t))
    return path

LIVE, PLAIN, GONE = "11111111-live", "22222222-plain", "33333333-gone"
transcript("-wt-shop", LIVE, wt,
           ["add structured data", "Done: the JSON-LD block is in place.", "now the canonical url"], 60)
write(os.path.join(projects, "-wt-shop", LIVE + ".jsonl"),
      open(os.path.join(projects, "-wt-shop", LIVE + ".jsonl")).read()
      + compact({"type": "ai-title", "aiTitle": "Structured data markup"}) + "\n")
os.utime(os.path.join(projects, "-wt-shop", LIVE + ".jsonl"), (time.time() - 60, time.time() - 60))
transcript("-dev-tool", PLAIN, tool, ["rewrite the cache layer", "Here is the plan for the cache."], 7200)
transcript("-wt-removed", GONE, removed, ["old work"], 3 * 86400)

calls_log = os.path.join(SANDBOX, "calls.log")
claude = write(os.path.join(SANDBOX, "claude"),
               '#!/bin/sh\nprintf "claude|%%s|%%s\\n" "$PWD" "$*" >> "%s"\n' % calls_log)
herdr = write(os.path.join(SANDBOX, "herdr"), """#!/bin/sh
printf 'herdr|%%s\\n' "$*" >> "%s"
case "$1 $2" in
  "pane list") printf '{"result":{"panes":[{"pane_id":"w1:p1","workspace_id":"w1","cwd":"%s","agent":"claude","agent_session":{"value":"%s"}}]}}' ;;
  "workspace list") printf '{"result":{"workspaces":[]}}' ;;
  "workspace create") printf '{"result":{"root_pane":{"pane_id":"w7:p1"}}}' ;;
  *) printf '{}' ;;
esac
""" % (calls_log, wt, LIVE))
for stub in (claude, herdr):
    os.chmod(stub, 0o755)

QUERIES = [(b"\x1b]11;?", b"\x1b]11;rgb:0000/0000/0000\x1b\\"), (b"\x1b]10;?", b"\x1b]10;rgb:ffff/ffff/ffff\x1b\\"),
           (b"\x1b[6n", b"\x1b[1;1R"), (b"\x1b[c", b"\x1b[?62c")]

failures = []
def check(cond, msg):
    print(("  ok   " if cond else "  FAIL ") + msg)
    if not cond: failures.append(msg)

class Session:
    """One run of the binary on a pty."""
    def __init__(self, in_herdr, cwd=SANDBOX, args=()):
        env = dict(os.environ, TERM="xterm-256color", COLORTERM="truecolor", HOME=home,
                   CLAUDE_PROJECTS_DIR=projects, CLAUDE_SESSIONS_CMD=claude, HERDR_BIN_PATH=herdr,
                   XDG_CONFIG_HOME=os.path.join(home, ".config"))
        for k in ("HERDR_ENV", "HERDR_PLUGIN_STATE_DIR", "HERDR_PLUGIN_ENTRYPOINT_ID", "HERDR_PLUGIN_CONTEXT_JSON"):
            env.pop(k, None)
        if in_herdr:
            env["HERDR_ENV"] = "1"
        self.master, slave = pty.openpty()
        fcntl.ioctl(slave, termios.TIOCSWINSZ, struct.pack("HHHH", ROWS, COLS, 0, 0))
        self.proc = subprocess.Popen([BIN, *args], stdin=slave, stdout=slave, stderr=slave, env=env,
                                     close_fds=True, cwd=cwd)
        os.close(slave)
        # A failed assertion must not leave the binary running on a dead pty.
        atexit.register(lambda p=self.proc: p.poll() is None and p.kill())
        self.screen = pyte.Screen(COLS, ROWS)
        self.stream = pyte.ByteStream(self.screen)
        self.raw = bytearray()
        self.answered = 0
        if os.path.exists(calls_log): os.remove(calls_log)

    def pump(self, seconds):
        end = time.time() + seconds
        while True:
            left = end - time.time()
            if left <= 0: break
            r, _, _ = select.select([self.master], [], [], left)
            if not r: continue
            try:
                data = os.read(self.master, 65536)
            except OSError:
                break
            if not data: break
            self.raw.extend(data); self.stream.feed(data)
            tail = bytes(self.raw[self.answered:])
            for q, reply in QUERIES:
                for _ in range(tail.count(q)):
                    os.write(self.master, reply)
            self.answered = len(self.raw)

    def repaint(self):
        # The v2 renderer updates the screen with scroll regions and SU, which
        # pyte ignores; a resize forces a full redraw it can follow.
        for cols in (COLS - 1, COLS):
            fcntl.ioctl(self.master, termios.TIOCSWINSZ, struct.pack("HHHH", ROWS, cols, 0, 0))
            self.screen.resize(ROWS, cols)
            if self.proc.poll() is None: os.kill(self.proc.pid, signal.SIGWINCH)
            self.pump(0.3)

    def frame(self):
        return [line.rstrip() for line in self.screen.display]

    def send(self, b, wait=0.4):
        os.write(self.master, b); self.pump(wait); self.repaint()
        return self.frame()

    def start(self):
        for _ in range(50):
            self.pump(0.1)
            if "asgotosession ❯" in "\n".join(self.frame()): break
        self.pump(0.5); self.repaint()
        return self.frame()

    def finish(self):
        try:
            self.proc.wait(timeout=3)
        except subprocess.TimeoutExpired:
            self.proc.kill()
            return None
        self.pump(0.2)
        return self.proc.returncode

    def calls(self, wait=0.0):
        time.sleep(wait)  # the detached focus helper logs after the binary exits
        if not os.path.exists(calls_log): return []
        return open(calls_log).read().splitlines()

# One frame (see frame.go): top border with the counter, input, main edge,
# list | preview, bottom edge, help, border. There is no context line.
INNER = COLS - 2
def listw(): return max(COLS - 3 - (COLS - 2) * 75 // 100, 10)   # the default split: list 25%, preview 75%
def divider(f): return next(l for l in f if l.startswith("├") and "┬" in l).index("┬")
SHIFT_RIGHT, SHIFT_LEFT = b"\x1b[1;2C", b"\x1b[1;2D"
def main(f):  return f[3:-3]
def left(f):  return [l[1:1 + listw()].rstrip() for l in main(f)]
def right(f): return [l[listw() + 3:-1].rstrip() for l in main(f)]
# The input line: the prompt and what is typed (or the placeholder). A build
# that is not a release says "(dev)" at the end of the edge over the
# input; devmark() says so.
def prompt(f): return f[1].strip("│ ").rstrip().removesuffix("(dev)").rstrip()
def devmark(f): return any(l.rstrip("╮┤─ ").endswith("(dev)") for l in f[:4])
def counter(f):
    for l in f:
        m = re.match(r"├─+ (\d+/\d+) ─[┴┤]", l)
        if m: return m.group(1)
    return ""
def status(f): return f[0].strip("╭╮─ ").removesuffix("(dev)").rstrip()
def helpline(f): return f[-2]
def dump(title, f):
    print("--- %s ---" % title)
    for i, l in enumerate(f): print("%2d|%s" % (i, l))

CTRL_A, ESC, ENTER, TAB, DOWN = b"\x01", b"\x1b", b"\r", b"\t", b"\x1b[B"

print("== asgotosession pty driver (%dx%d) ==" % (COLS, ROWS))

# ---------- run 1: outside herdr: list, preview, filter, resume in place ----------
s = Session(in_herdr=False)
f = s.start(); dump("plain run", f)
check(prompt(f) == "asgotosession ❯ Search by title, directory, branch…", "prompt line is clean: %r" % f[1])
check(devmark(f), "a dev build says so on the edge over the input")
check(f[0].startswith("╭") and f[-1].startswith("╰") and f[2].startswith("├") and "┬" in f[2],
      "one frame: the input sits right under the top border, no title line")
check(b"\x1b[?1049h" in s.raw, "program entered the alt screen")
rows = [l for l in left(f) if l.strip()]
check(len(rows) == 2, "sessions of removed directories are hidden: %d rows" % len(rows))
check(rows[0].startswith("▌ Structured data markup") and rows[0].endswith("1m"),
      "newest first, ai title and age shown: %r" % rows[0])
check("rewrite the cache layer" in rows[1], "an untitled session shows its first prompt: %r" % rows[1])
check("●" not in rows[0], "no live marks outside herdr")
prev = "\n".join(right(f))
check("❯ now the canonical url" in prev and "Done: the JSON-LD block" in prev, "preview shows the conversation")
check(counter(f) == "2/2" and "enter resume" in helpline(f), "counter %r and help %r" % (counter(f), helpline(f)))

f = s.send(CTRL_A); rows = [l for l in left(f) if l.strip()]
check(len(rows) == 3 and "old work" in rows[2], "ctrl+a lists missing directories: %d rows" % len(rows))
f = s.send(DOWN + DOWN + ENTER, 0.5)
check(s.proc.poll() is None and "no longer exists" in helpline(f), "a missing directory is not resumed: %r" % helpline(f))
f = s.send(CTRL_A + b"cache", 0.5); dump("filtered", f)
rows = [l for l in left(f) if l.strip()]
check(len(rows) == 1 and rows[0].startswith("▌") and "cache layer" in rows[0], "typing filters: %r" % rows)
check(counter(f) == "1/2", "the counter follows the filter: %r" % counter(f))
s.send(ENTER, 0.3)
check(s.finish() == 0, "clean exit after enter")
check(s.calls() == ["claude|%s|--resume %s" % (tool, PLAIN)], "claude resumed in the session directory: %r" % s.calls())

# ---------- run 2: inside herdr: live mark, focus the live pane ----------
s = Session(in_herdr=True)
f = s.start(); dump("herdr run", f)
rows = [l for l in left(f) if l.strip()]
check(rows[0].startswith("▌●"), "the live session is marked: %r" % rows[0])
check("live in pane w1:p1" in "\n".join(right(f)), "preview names the pane")
s.send(ENTER, 0.3)
check(s.finish() == 0, "clean exit after enter")
calls = s.calls()
check("herdr|agent focus w1:p1" in calls and not any("pane run" in c for c in calls),
      "a live session is focused, not started again: %r" % calls)

# ---------- run 3: inside herdr: no space for the directory, create it ----------
s = Session(in_herdr=True)
s.start()
s.send(DOWN + ENTER, 0.3)
check(s.finish() == 0, "clean exit after enter")
calls = s.calls(wait=1.0)
check("herdr|workspace create --cwd %s --label tool --focus" % tool in calls, "the space is created: %r" % calls)
check("herdr|pane run w7:p1 %s --resume %s" % (claude, PLAIN) in calls, "the resume command runs in the new pane")
check("herdr|agent focus w7:p1" in calls, "the detached helper focuses the agent pane")

# ---------- run 4: -here + initial query, ctrl+a widens, q quits ----------
s = Session(in_herdr=False, cwd=tool, args=("-here",))
f = s.start(); dump("-here", f)
rows = [l for l in left(f) if l.strip()]
check(len(rows) == 1 and "cache layer" in rows[0], "-here narrows to the current directory: %r" % rows)
check("^a everywhere" in helpline(f) and counter(f) == "1/1" and status(f).startswith("[in "), "help offers to widen and the scope sits on the top border: %r %r" % (counter(f), status(f)))
f = s.send(CTRL_A); rows = [l for l in left(f) if l.strip()]
check(len(rows) == 2, "ctrl+a lists everywhere: %d rows" % len(rows))
s.send(b"q", 0.2)
check(s.finish() == 0 and s.calls() == [], "q quits with an empty filter and resumes nothing")

# ---------- run 5: the divider moves and stays where it was left ----------
s = Session(in_herdr=False)
f = s.start(); at = divider(f)
f = s.send(SHIFT_RIGHT, 0.6); grown = divider(f)
check(grown > at and all(len(l) == COLS for l in f), "shift+right grows the list: %d -> %d" % (at, grown))
f = s.send(SHIFT_LEFT, 0.6)
check(divider(f) == at, "shift+left shrinks it back: %d" % divider(f))
for _ in range(5): f = s.send(SHIFT_RIGHT, 0.3)
row = f[3][1:divider(f)]
check("Structured data markup" in row and "ESHOP-551-structured-data" in row, "a wide list shows the directory tail too: %r" % row)
for _ in range(5): s.send(SHIFT_LEFT, 0.3)
s.send(SHIFT_RIGHT, 0.6)
s.send(b"q", 0.2); s.finish()
s = Session(in_herdr=False)
f = s.start()
check(divider(f) == grown, "the next run opens with the same split: %d" % divider(f))
s.send(SHIFT_LEFT, 0.6)
s.send(b"q", 0.2); s.finish()

shutil.rmtree(SANDBOX, ignore_errors=True)
print("\n%d failure(s)" % len(failures))
sys.exit(1 if failures else 0)
