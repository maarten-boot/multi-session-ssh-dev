package idle

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"session-workbench/internal/profile"
	"session-workbench/internal/remote"
)

func TestSettings(t *testing.T) {
	var s struct {
		Hooks map[string][]struct {
			Matcher string `json:"matcher"`
			Hooks   []struct{ Type, Command string }
		} `json:"hooks"`
	}
	raw := Settings("s-abc", true)
	if err := json.Unmarshal([]byte(raw), &s); err != nil {
		t.Fatal(err)
	}
	for _, ev := range []string{"SessionStart", "UserPromptSubmit", "Stop", "Notification", "SessionEnd"} {
		h := s.Hooks[ev]
		if len(h) != 1 || len(h[0].Hooks) != 1 || !strings.Contains(h[0].Hooks[0].Command, " "+ev+" $PPID") ||
			!strings.Contains(h[0].Hooks[0].Command, `/claude"`) || !strings.Contains(h[0].Hooks[0].Command, `"$d/s-abc.tmp"`) {
			t.Errorf("%s: %+v", ev, h)
		}
	}
	if s.Hooks["Notification"][0].Matcher != "permission_prompt|elicitation_dialog" || s.Hooks["Stop"][0].Matcher != "" {
		t.Error("matchers")
	}
	// Safe in any shell's single quotes (fish reads \\ and \' there).
	if strings.ContainsAny(raw, `'`) || strings.Contains(raw, `\\`) {
		t.Errorf("quotes or double backslashes: %s", raw)
	}
}

func TestWithHooks(t *testing.T) {
	arg := " --settings " + remote.Quote(Settings("s1", false))
	for _, c := range []struct {
		in, want string
		runs     bool
	}{
		{"claude --continue || claude", "claude" + arg + " --continue || claude" + arg, true},
		{"claude", "claude" + arg, true},
		{"FOO=1 exec ~/.local/bin/claude --model opus", "FOO=1 exec ~/.local/bin/claude" + arg + " --model opus", true},
		{"cd sub && claude;echo done", "cd sub && claude" + arg + ";echo done", true},
		{"echo claude", "echo claude", false},
		{"claudette", "claudette", false},
		{"", "", false},
		{"bash", "bash", false},
		{`echo 'a; claude' && claude -p "x || y"`, `echo 'a; claude' && claude` + arg + ` -p "x || y"`, true},
	} {
		if got := WithHooks(c.in, "s1", false); got != c.want {
			t.Errorf("WithHooks(%q) = %q", c.in, got)
		}
		if Runs(c.in) != c.runs {
			t.Errorf("Runs(%q) != %v", c.in, c.runs)
		}
	}
}

func TestResumeCommand(t *testing.T) {
	arg := "--settings " + remote.Quote(Settings("s1", false))
	for _, c := range []struct{ in, want string }{
		{"claude --continue || claude", "claude " + arg + " --resume 'abc-1'"},
		{"claude -c --model opus", "claude " + arg + " --resume 'abc-1' --model opus"},
		{"claude --resume old --fork-session --effort high", "claude " + arg + " --resume 'abc-1' --effort high"},
		{"claude -r --model x", "claude " + arg + " --resume 'abc-1' --model x"},
		{"claude --session-id 1234 --resume=x 'a b'", "claude " + arg + " --resume 'abc-1' 'a b'"},
		{"~/bin/claude ; echo", "~/bin/claude " + arg + " --resume 'abc-1'"},
	} {
		if got := ResumeCommand(c.in, "s1", "abc-1", false); got != c.want {
			t.Errorf("ResumeCommand(%q)\n= %q\nwant %q", c.in, got, c.want)
		}
	}
	// No conversation yet: the start command again.
	if got := ResumeCommand("claude --continue || claude", "s1", "", false); got != WithHooks("claude --continue || claude", "s1", false) {
		t.Error(got)
	}
}

func TestParseAndIdle(t *testing.T) {
	out := "s1\t1000\t600\tStop\t42\t%3\tconv-1\t1\tcw-s1\t650\t1\t0\t0\t\t0\t\n" +
		"garbage\n" +
		"s2\t1000\t0\t\t0\t\t\t0\t\t0\t0\t0\t1\tconv-2\t1\t%7\n"
	list := Parse(out)
	if len(list) != 2 {
		t.Fatalf("%+v", list)
	}
	s := list[0]
	if s.ID != "s1" || s.Now != 1000 || s.At != 600 || s.Event != "Stop" || s.PID != 42 || s.Pane != "%3" ||
		s.Conv != "conv-1" || !s.Transcript || s.PaneSession != "cw-s1" || s.Activity != 650 || !s.Alive || s.Busy != 0 || s.Stopped {
		t.Errorf("%+v", s)
	}
	if st := list[1]; !st.Stopped || st.StoppedConv != "conv-2" || !st.StoppedTx || st.StoppedPane != "%7" {
		t.Errorf("%+v", st)
	}
	// The window's last output (650) counts: 350 s quiet.
	if !s.Idle("cw-s1", 5*time.Minute) || s.Idle("cw-s1", 6*time.Minute) || s.Idle("cw-s1", 0) {
		t.Error("idle time")
	}
	for name, mod := range map[string]func(*State){
		"busy":        func(s *State) { s.Busy = 1 },
		"dead":        func(s *State) { s.Alive = false },
		"working":     func(s *State) { s.Event = "UserPromptSubmit" },
		"asking":      func(s *State) { s.Event = "Notification" },
		"ended":       func(s *State) { s.Event = "SessionEnd" },
		"other tmux":  func(s *State) { s.PaneSession = "cw-other" },
		"stopped":     func(s *State) { s.Stopped = true },
		"typed in it": func(s *State) { s.Activity = 900 },
	} {
		c := s
		mod(&c)
		if c.Idle("cw-s1", 5*time.Minute) {
			t.Errorf("%s: idle", name)
		}
	}
	if c := s; func() bool { c.Event = "SessionStart"; return c.Idle("cw-s1", 5*time.Minute) }() == false {
		t.Error("just started and untouched: idle")
	}
}

func TestScripts(t *testing.T) {
	if _, err := CheckScript([]string{"s1", "bad id"}); err == nil {
		t.Error("bad id accepted")
	}
	st := State{ID: "s1", PID: 42, Pane: "%3", Conv: "c-1", Transcript: true}
	s, err := StopScript(st, "cw-s1", "~/proj", 5*time.Minute, IdleNotice(5*time.Minute))
	if err != nil || !strings.Contains(s, "respawn-pane -k -t %3 -c \"$HOME\"/") && strings.Contains(s, "pid=42;") || !strings.Contains(s, "-ge 300 ]") ||
		!strings.Contains(s, "'c-1 1 %3'") || !strings.Contains(s, "after 5 min without input") {
		t.Errorf("%v\n%s", err, s)
	}
	for _, bad := range []State{{ID: "s1", PID: 42, Pane: "3"}, {ID: "s1", PID: 0, Pane: "%3"}, {ID: "s1", PID: 42, Pane: "%3", Conv: "a;b"}} {
		if _, err := StopScript(bad, "cw-s1", "/p", time.Minute, NowNotice); err == nil {
			t.Errorf("accepted %+v", bad)
		}
	}
	if _, err := ResumeScript(State{ID: "s1", StoppedPane: "%3;x"}, "cw-s1", "/p", "claude"); err == nil {
		t.Error("bad pane accepted")
	}
	if Human(5*time.Minute) != "5 min" || Human(2*time.Hour) != "2 h" || Human(90*time.Minute) != "1 h 30 min" {
		t.Error("Human")
	}
}

// TestStatusLine runs the status line of Settings(id, true) as Claude Code
// would (sh, the data on stdin) with a fake home (SPEC F18): the usage is
// saved only from data that has it, and the user's own status line runs
// with the same data, else the usage is shown.
func TestStatusLine(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the agent host runs sh")
	}
	var s struct {
		StatusLine struct{ Type, Command string } `json:"statusLine"`
	}
	if err := json.Unmarshal([]byte(Settings("s1", true)), &s); err != nil || s.StatusLine.Type != "command" {
		t.Fatalf("%v %+v", err, s)
	}
	if strings.Contains(Settings("s1", false), "statusLine") {
		t.Error("a status line without usage")
	}
	home := t.TempDir()
	dir := filepath.Join(home, ".local", "state", profile.Dir(), "claude")
	os.MkdirAll(dir, 0o755)
	os.WriteFile(filepath.Join(dir, "statusline"), []byte(StatusHelper), 0o755)
	run := func(input string) string {
		t.Helper()
		cmd := exec.Command("sh", "-c", s.StatusLine.Command)
		cmd.Env = append(os.Environ(), "HOME="+home)
		cmd.Stdin = strings.NewReader(input)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("%v: %s", err, out)
		}
		return strings.TrimSpace(string(out))
	}
	with := `{"model":{"display_name":"Opus"},"rate_limits":{"five_hour":{"used_percentage":23.5,"resets_at":1738425600},"seven_day":{"resets_at":1738857600,"used_percentage":41.2}}}`
	if got := run(with); got != "5h 23% · 7d 41%" {
		t.Errorf("usage shown: %q", got)
	}
	usage := filepath.Join(dir, UsageFile)
	if b, _ := os.ReadFile(usage); string(b) != with {
		t.Errorf("saved: %q", b)
	}
	if got := run(`{"model":{"display_name":"Opus"}}`); got != "" {
		t.Errorf("no usage yet: %q", got)
	}
	if b, _ := os.ReadFile(usage); string(b) != with {
		t.Error("data without the usage replaced it")
	}
	// The user's own status line, with the same data.
	os.MkdirAll(filepath.Join(home, ".claude"), 0o755)
	os.WriteFile(filepath.Join(home, ".claude", "settings.json"),
		[]byte(`{"statusLine": {"type": "command", "command": "grep -o '\"display_name\":\"[^\"]*\"'"}}`), 0o644)
	if got := run(with); got != `"display_name":"Opus"` {
		t.Errorf("own status line: %q", got)
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 2 {
		t.Errorf("left behind: %v", entries)
	}
}
