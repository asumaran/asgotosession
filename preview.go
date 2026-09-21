package main

// Session preview for the right-hand column: a header (title, directory,
// branch, id) and the tail of the conversation, prompts and replies only.
// Transcripts run to tens of megabytes, so only the last previewTailBytes are
// read, off the update loop as a tea.Cmd; renders are cached per (file,
// width, mtime) so a session that kept going re-renders on its own.

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"os"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

const (
	previewTailBytes = 2 * 1024 * 1024
	previewMaxTurns  = 40
	previewTurnRunes = 1200
)

type previewMsg struct {
	key     string
	content string
}

// previewKey identifies a render. The mtime component makes stale renders
// unreachable after the transcript grows; a missing file has mtime 0.
func previewKey(file string, width int) string {
	var mtime int64
	if st, err := os.Stat(file); err == nil {
		mtime = st.ModTime().UnixNano()
	}
	return file + "|" + strconv.Itoa(width) + "|" + strconv.FormatInt(mtime, 10)
}

func renderPreviewCmd(s session, key string, width int) tea.Cmd {
	return func() tea.Msg {
		return previewMsg{key: key, content: renderSession(&s, width)}
	}
}

// turn is one side of the conversation.
type turn struct {
	user bool
	text string
}

// previewHeader is the instant block above the conversation: it comes from
// what the list already knows, so it is there before the transcript is read,
// and it stays put while the conversation scrolls. rightLines puts one blank
// line under it, as in every tool of the family with a header.
func previewHeader(s *session, width int, home string) string {
	lines := []string{stHeader.Render(truncate(s.label(), width))}
	dir := tildePath(s.cwd, home)
	if s.missing {
		dir += " [missing]"
	}
	lines = append(lines, stDim.Render(truncate(dir, width)))
	meta := s.last.Format("02/01 15:04") + " · " + s.id
	if s.branch != "" {
		meta = s.branch + " · " + meta
	}
	lines = append(lines, stDim.Render(truncate(meta, width)))
	if s.pane != "" {
		lines = append(lines, stLive.Render(truncate("● live in pane "+s.pane, width)))
	}
	return strings.Join(lines, "\n")
}

// renderSession builds the conversation of one session, the part of the
// right column that scrolls.
func renderSession(s *session, width int) string {
	var b strings.Builder
	turns, cut, err := readTail(s.file)
	switch {
	case err != nil:
		b.WriteString(stError.Render(truncate(err.Error(), width)))
		return b.String()
	case len(turns) == 0:
		b.WriteString(stDim.Render("(no conversation yet)"))
		return b.String()
	}
	if cut {
		b.WriteString(stDim.Render("… earlier turns left out") + "\n\n")
	}
	for i, t := range turns {
		if i > 0 {
			b.WriteString("\n\n")
		}
		if t.user {
			b.WriteString(styleLines(wrapText("❯ "+t.text, width), stUser.Render))
		} else {
			b.WriteString(wrapText(t.text, width))
		}
	}
	return b.String()
}

// readTail returns the last previewMaxTurns turns found in the last
// previewTailBytes of the transcript, and whether anything came before them.
func readTail(file string) (turns []turn, cut bool, err error) {
	f, err := os.Open(file)
	if err != nil {
		return nil, false, err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return nil, false, err
	}
	var r io.Reader = f
	if st.Size() > previewTailBytes {
		if _, err := f.Seek(-previewTailBytes, io.SeekEnd); err != nil {
			return nil, false, err
		}
		cut = true
	}
	br := bufio.NewReaderSize(r, 256*1024)
	if cut {
		_, _ = br.ReadBytes('\n') // the seek landed mid-line
	}
	for {
		line, rerr := br.ReadBytes('\n')
		if len(line) > 0 {
			turns = append(turns, turnsOf(line)...)
		}
		if rerr != nil {
			break
		}
	}
	if len(turns) > previewMaxTurns {
		turns, cut = turns[len(turns)-previewMaxTurns:], true
	}
	return turns, cut, nil
}

var (
	userPre      = []byte(`"type":"user"`)
	assistantPre = []byte(`"type":"assistant"`)
)

// turnsOf decodes one transcript line into the turns worth showing: what the
// user typed and what the assistant said. Tool calls, tool results, harness
// entries and subagent chatter are left out.
func turnsOf(line []byte) []turn {
	if !bytes.Contains(line, userPre) && !bytes.Contains(line, assistantPre) {
		return nil
	}
	var m message
	if json.Unmarshal(line, &m) != nil || m.IsSidechain {
		return nil
	}
	var out []turn
	for _, t := range m.texts() {
		switch m.Type {
		case "user":
			if typedByUser(t) {
				out = append(out, turn{user: true, text: clip(t)})
			}
		case "assistant":
			if strings.TrimSpace(t) != "" {
				out = append(out, turn{text: clip(t)})
			}
		}
	}
	return out
}

// clip bounds one turn so a pasted log does not push everything else away.
func clip(text string) string {
	text = strings.TrimSpace(text)
	if r := []rune(text); len(r) > previewTurnRunes {
		return string(r[:previewTurnRunes]) + "…"
	}
	return text
}

// wrapText prepares raw text for the viewport: tabs expanded, control
// characters dropped, lines wrapped at width.
func wrapText(text string, width int) string {
	lines := strings.Split(text, "\n")
	for i, l := range lines {
		l = strings.ReplaceAll(l, "\t", "    ")
		lines[i] = strings.Map(func(r rune) rune {
			if r < 0x20 || r == 0x7f {
				return -1
			}
			return r
		}, l)
	}
	return ansi.Wrap(strings.Join(lines, "\n"), width, "")
}

// styleLines styles a block line by line: lipgloss pads a multi-line block to
// its widest line.
func styleLines(block string, style func(...string) string) string {
	lines := strings.Split(block, "\n")
	for i, l := range lines {
		lines[i] = style(l)
	}
	return strings.Join(lines, "\n")
}
