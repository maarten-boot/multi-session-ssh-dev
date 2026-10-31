package qa

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// Configure is applied to every program the Q&A tab starts (hides console
// windows on Windows).
var Configure = func(*exec.Cmd) {}

// ClaudeModels are the models offered for Claude Code: its own default and
// its names for the latest of each line.
var ClaudeModels = []Model{
	{ID: "", Name: "Claude Code's default"},
	{ID: "fable", Name: "Fable (latest)"},
	{ID: "opus", Name: "Opus (latest)"},
	{ID: "sonnet", Name: "Sonnet (latest)"},
	{ID: "haiku", Name: "Haiku (latest)"},
}

// FindClaude returns the Claude Code program: program itself when set (a
// leading ~ is the home folder), else claude on PATH or in the folders it
// installs to (a desktop session's PATH often lacks ~/.local/bin).
func FindClaude(program string) (string, error) {
	home, _ := os.UserHomeDir()
	if program != "" {
		if program == "~" || strings.HasPrefix(program, "~/") || strings.HasPrefix(program, `~\`) {
			program = filepath.Join(home, program[1:])
		}
		p, err := exec.LookPath(program)
		if err != nil {
			return "", fmt.Errorf("Claude Code (qa_claude = %q) not found", program)
		}
		return p, nil
	}
	if p, err := exec.LookPath("claude"); err == nil {
		return p, nil
	}
	exe := "claude"
	if runtime.GOOS == "windows" {
		exe = "claude.exe"
	}
	for _, p := range []string{
		filepath.Join(home, ".local", "bin", exe),
		filepath.Join(home, ".claude", "local", exe),
		"/opt/homebrew/bin/claude",
		"/usr/local/bin/claude",
	} {
		if fi, err := os.Stat(p); err == nil && !fi.IsDir() {
			return p, nil
		}
	}
	return "", errors.New("Claude Code (claude) not found: install it, or set qa_claude in the config to the program")
}

// ClaudeCode answers through Claude Code (`claude -p`), with Claude Code's
// own login: no API key needed. It runs with no tools (it only answers)
// and without settings, hooks or MCP servers, in Dir (an empty folder of
// the app: Claude Code keeps its sessions per folder). The conversation is
// Claude Code's own session, resumed with each question.
type ClaudeCode struct {
	Program string // as for FindClaude
	Model   string // "" = Claude Code's default
	Dir     string
}

// ccLine is a line of Claude Code's stream-json output (the parts used).
type ccLine struct {
	Type      string `json:"type"`
	Subtype   string `json:"subtype"`
	Model     string `json:"model"`
	SessionID string `json:"session_id"`
	Parent    string `json:"parent_tool_use_id"`
	Event     *struct {
		Type  string `json:"type"`
		Delta struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"delta"`
	} `json:"event"`
	Result     string `json:"result"`
	IsError    bool   `json:"is_error"`
	StopReason string `json:"stop_reason"`
	Usage      *struct {
		Input      int64 `json:"input_tokens"`
		Output     int64 `json:"output_tokens"`
		CacheRead  int64 `json:"cache_read_input_tokens"`
		CacheWrite int64 `json:"cache_creation_input_tokens"`
	} `json:"usage"`
}

// Args is the command line after the program: resume names the session
// to continue, or "" for a new one (newID).
func (cc ClaudeCode) Args(resume, newID string) []string {
	args := []string{"-p", "--output-format", "stream-json", "--verbose", "--include-partial-messages",
		"--tools", "", "--system-prompt", system, "--strict-mcp-config", "--disable-slash-commands",
		"--setting-sources", ""}
	if cc.Model != "" {
		args = append(args, "--model", cc.Model)
	}
	if resume != "" {
		return append(args, "--resume", resume)
	}
	return append(args, "--session-id", newID)
}

func (cc ClaudeCode) Ask(ctx context.Context, c Conversation, question string, delta func(string)) Turn {
	t := Turn{Time: time.Now(), Question: question, Engine: EngineClaude, Model: cc.Model}
	program, err := FindClaude(cc.Program)
	if err != nil {
		t.Error = err.Error()
		return t
	}
	resume := ""
	for _, old := range c.Turns {
		if old.Session != "" {
			resume = old.Session
		}
	}
	if err := os.MkdirAll(cc.Dir, 0o700); err != nil {
		t.Error = err.Error()
		return t
	}
	cmd := exec.CommandContext(ctx, program, cc.Args(resume, newUUID())...)
	cmd.Dir = cc.Dir
	cmd.Stdin = strings.NewReader(question)
	var stderr tail
	cmd.Stderr = &stderr
	Configure(cmd)
	out, err := cmd.StdoutPipe()
	if err != nil {
		t.Error = err.Error()
		return t
	}
	if err := cmd.Start(); err != nil {
		t.Error = "Claude Code did not start: " + err.Error()
		return t
	}
	var text strings.Builder
	var res *ccLine
	sc := bufio.NewScanner(out)
	sc.Buffer(make([]byte, 64*1024), 16*1024*1024)
	for sc.Scan() {
		var l ccLine
		if json.Unmarshal(sc.Bytes(), &l) != nil {
			continue
		}
		switch {
		case l.Type == "system" && l.Subtype == "init" && l.Model != "":
			t.Model = l.Model
		case l.Type == "stream_event" && l.Parent == "" && l.Event != nil &&
			l.Event.Type == "content_block_delta" && l.Event.Delta.Type == "text_delta":
			text.WriteString(l.Event.Delta.Text)
			delta(l.Event.Delta.Text)
		case l.Type == "result":
			res = &l
		}
	}
	io.Copy(io.Discard, out)
	werr := cmd.Wait()
	t.Answer = text.String()
	switch {
	case ctx.Err() != nil:
		t.Error = "stopped"
		return t
	case res == nil && werr != nil:
		t.Error = "Claude Code ended (" + werr.Error() + ")"
		if s := stderr.lastLine(); s != "" {
			t.Error += ": " + s
		}
		return t
	case res == nil:
		t.Error = "Claude Code gave no answer"
		return t
	}
	if res.Usage != nil {
		t.Usage = Usage{Input: res.Usage.Input, Output: res.Usage.Output, CacheRead: res.Usage.CacheRead, CacheWrite: res.Usage.CacheWrite}
	}
	if t.Answer == "" && !res.IsError {
		t.Answer = res.Result // no partial messages came
	}
	switch {
	case res.IsError:
		msg := strings.TrimSpace(res.Result)
		if msg == "" {
			msg = res.Subtype
		}
		t.Error = "Claude Code: " + msg
		if strings.Contains(msg, "No conversation found") {
			t.Error += " (New conversation starts over)"
		} else if l := strings.ToLower(msg); strings.Contains(l, "/login") || strings.Contains(l, "not logged in") {
			t.Error += " (run claude in a terminal once and log in)"
		}
	case res.StopReason == "refusal":
		t.Error = "Claude declined to answer"
	case res.StopReason == "max_tokens":
		t.Error = "the answer was cut off at the length limit"
	}
	if res.SessionID != "" && !strings.Contains(t.Error, "No conversation found") {
		t.Session = res.SessionID
	}
	return t
}

// newUUID is a random (version 4) UUID.
func newUUID() string {
	var b [16]byte
	rand.Read(b[:])
	b[6] = b[6]&0x0f | 0x40
	b[8] = b[8]&0x3f | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:])
}

// tail keeps the last 4 KB written to it (a program's stderr).
type tail struct{ b []byte }

func (w *tail) Write(p []byte) (int, error) {
	w.b = append(w.b, p...)
	if len(w.b) > 4096 {
		w.b = w.b[len(w.b)-4096:]
	}
	return len(p), nil
}

// lastLine is the last non-empty line written.
func (w *tail) lastLine() string {
	lines := strings.Split(strings.TrimSpace(strings.ReplaceAll(string(w.b), "\r\n", "\n")), "\n")
	return strings.TrimSpace(lines[len(lines)-1])
}
