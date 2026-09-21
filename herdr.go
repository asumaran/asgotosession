package main

// Everything that talks to herdr, through its CLI. Resuming happens AFTER the
// TUI exits (quitting is what closes the popup): go to the pane where the
// session is already live, or reuse an idle shell in the space of its
// directory, or open a tab there, or create the space.

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

// runner executes a herdr CLI command and returns its stdout.
type runner func(args ...string) ([]byte, error)

func inHerdr() bool {
	return os.Getenv("HERDR_ENV") == "1"
}

// ---- herdr CLI JSON shapes ----

type paneInfo struct {
	PaneID       string  `json:"pane_id"`
	WorkspaceID  string  `json:"workspace_id"`
	Cwd          string  `json:"cwd"`
	Agent        *string `json:"agent"`
	AgentSession *struct {
		Value string `json:"value"`
	} `json:"agent_session"`
}

type workspaceInfo struct {
	WorkspaceID string `json:"workspace_id"`
	Worktree    *struct {
		CheckoutPath string `json:"checkout_path"`
	} `json:"worktree"`
}

func listPanes(run runner) ([]paneInfo, error) {
	out, err := run("pane", "list")
	if err != nil {
		return nil, err
	}
	var resp struct {
		Result struct {
			Panes []paneInfo `json:"panes"`
		} `json:"result"`
	}
	if err := json.Unmarshal(out, &resp); err != nil {
		return nil, err
	}
	return resp.Result.Panes, nil
}

func listWorkspaces(run runner) ([]workspaceInfo, error) {
	out, err := run("workspace", "list")
	if err != nil {
		return nil, err
	}
	var resp struct {
		Result struct {
			Workspaces []workspaceInfo `json:"workspaces"`
		} `json:"result"`
	}
	if err := json.Unmarshal(out, &resp); err != nil {
		return nil, err
	}
	return resp.Result.Workspaces, nil
}

// liveSessions maps session id → pane for the sessions running right now.
func liveSessions(run runner) map[string]string {
	live := map[string]string{}
	panes, err := listPanes(run)
	if err != nil {
		return live
	}
	for _, p := range panes {
		if p.AgentSession != nil && p.AgentSession.Value != "" {
			live[p.AgentSession.Value] = p.PaneID
		}
	}
	return live
}

// isIdleShell reports whether nothing but the shell runs in the pane.
func isIdleShell(run runner, pane string) bool {
	out, err := run("pane", "process-info", "--pane", pane)
	if err != nil {
		return false
	}
	var resp struct {
		Result struct {
			ProcessInfo struct {
				ForegroundPGID *int `json:"foreground_process_group_id"`
				ShellPID       *int `json:"shell_pid"`
			} `json:"process_info"`
		} `json:"result"`
	}
	if json.Unmarshal(out, &resp) != nil {
		return false
	}
	pi := resp.Result.ProcessInfo
	return pi.ForegroundPGID != nil && pi.ShellPID != nil && *pi.ForegroundPGID == *pi.ShellPID
}

// rootPaneID reads the pane a `tab create` / `workspace create` call made.
func rootPaneID(out []byte) (string, error) {
	var resp struct {
		Result struct {
			RootPane struct {
				PaneID string `json:"pane_id"`
			} `json:"root_pane"`
		} `json:"result"`
	}
	if err := json.Unmarshal(out, &resp); err != nil {
		return "", err
	}
	if resp.Result.RootPane.PaneID == "" {
		return "", fmt.Errorf("herdr did not report the new pane")
	}
	return resp.Result.RootPane.PaneID, nil
}

// ---- resuming ----

// claudeCommand is the command used to resume, as words.
// ASGOTOSESSION_OPENER replaces it, e.g. a wrapper or extra flags (opener.go).
func claudeCommand() []string {
	if f := openerArgv("asgotosession"); len(f) > 0 {
		return f
	}
	return []string{"claude"}
}

func resumeCommand(id string) string {
	return strings.Join(append(claudeCommand(), "--resume", id), " ")
}

// openInHerdr puts the session on screen and returns the pane that still has
// to take the focus once herdr recognizes the agent in it ("" when the
// session was already live and is focused already).
func openInHerdr(run runner, id, cwd string) (string, error) {
	panes, err := listPanes(run)
	if err != nil {
		return "", fmt.Errorf("herdr pane list: %w", err)
	}

	// Session already live in some pane: just go there.
	for _, p := range panes {
		if p.AgentSession != nil && p.AgentSession.Value == id {
			_, err := run("agent", "focus", p.PaneID)
			return "", err
		}
	}

	ws := ""
	if spaces, err := listWorkspaces(run); err == nil {
		for _, w := range spaces {
			if w.Worktree != nil && w.Worktree.CheckoutPath == cwd {
				ws = w.WorkspaceID
				break
			}
		}
	}
	if ws == "" {
		for _, p := range panes {
			if p.Cwd == cwd {
				ws = p.WorkspaceID
				break
			}
		}
	}

	pane := ""
	if ws != "" {
		for _, p := range panes {
			if p.WorkspaceID == ws && p.Cwd == cwd && p.Agent == nil && isIdleShell(run, p.PaneID) {
				pane = p.PaneID
				break
			}
		}
		if _, err := run("workspace", "focus", ws); err != nil {
			return "", fmt.Errorf("herdr workspace focus: %w", err)
		}
		if pane == "" {
			out, err := run("tab", "create", "--workspace", ws, "--cwd", cwd, "--label", "claude", "--focus")
			if err != nil {
				return "", fmt.Errorf("herdr tab create: %w", err)
			}
			if pane, err = rootPaneID(out); err != nil {
				return "", err
			}
		}
	} else {
		out, err := run("workspace", "create", "--cwd", cwd, "--label", filepath.Base(cwd), "--focus")
		if err != nil {
			return "", fmt.Errorf("herdr workspace create: %w", err)
		}
		if pane, err = rootPaneID(out); err != nil {
			return "", err
		}
	}

	if _, err := run("pane", "run", pane, resumeCommand(id)); err != nil {
		return "", fmt.Errorf("herdr pane run: %w", err)
	}
	return pane, nil
}

// awaitAgentFocus moves the focus onto the pane once herdr recognizes the
// agent; best effort.
func awaitAgentFocus(run runner, pane string) {
	for i := 0; i < 10; i++ {
		if _, err := run("agent", "focus", pane); err == nil {
			return
		}
		time.Sleep(500 * time.Millisecond)
	}
}

// detachAgentFocus runs awaitAgentFocus in a process of its own session, so
// the popup closes right away instead of sitting empty while claude boots.
func detachAgentFocus(pane string) {
	self, err := os.Executable()
	if err != nil {
		return
	}
	cmd := exec.Command(self, "-await-focus", pane)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	_ = cmd.Start()
}

// runResume is the post-quit work. Outside herdr there is no space to open:
// the process becomes claude, in the session's directory.
func runResume(s *session) error {
	if !dirExists(s.cwd) {
		return fmt.Errorf("%s no longer exists, cannot resume %s there", s.cwd, s.id)
	}
	if !inHerdr() {
		argv := append(claudeCommand(), "--resume", s.id)
		bin, err := exec.LookPath(argv[0])
		if err != nil {
			return err
		}
		if err := os.Chdir(s.cwd); err != nil {
			return err
		}
		return syscall.Exec(bin, argv, os.Environ())
	}
	pane, err := openInHerdr(herdrAct, s.id, s.cwd) // it may create a space: the long timeout
	if err != nil {
		return err
	}
	if pane != "" {
		detachAgentFocus(pane)
	}
	return nil
}
