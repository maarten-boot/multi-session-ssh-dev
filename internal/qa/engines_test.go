package qa

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/anthropics/anthropic-sdk-go/v2/option"
)

// TestMain lets the test binary stand in for Claude Code (QA_FAKE=claude)
// and for a Q&A command (QA_FAKE=cmd), so these tests run everywhere.
func TestMain(m *testing.M) {
	switch os.Getenv("QA_FAKE") {
	case "claude":
		fakeClaude()
	case "cmd":
		fakeCommand()
	default:
		os.Exit(m.Run())
	}
	os.Exit(0)
}

// fakeClaude answers like `claude -p --output-format stream-json`; it
// writes its arguments to $QA_FAKE_ARGS.
func fakeClaude() {
	args := os.Args[1:]
	os.WriteFile(os.Getenv("QA_FAKE_ARGS"), []byte(strings.Join(args, "\n")), 0o600)
	in, _ := io.ReadAll(os.Stdin)
	q := string(in)
	sid := ""
	for i, a := range args {
		if a == "--resume" || a == "--session-id" {
			sid = args[i+1]
		}
	}
	line := func(v any) { b, _ := json.Marshal(v); fmt.Println(string(b)) }
	switch q {
	case "crash":
		fmt.Fprintln(os.Stderr, "something\nboom")
		os.Exit(3)
	case "login":
		line(map[string]any{"type": "result", "subtype": "success", "is_error": true, "result": "Not logged in · Please run /login"})
		return
	case "slow":
		time.Sleep(10 * time.Second)
	}
	line(map[string]any{"type": "system", "subtype": "init", "model": "claude-fake-1", "session_id": sid})
	for _, p := range []string{"An", "swer: " + q} {
		line(map[string]any{"type": "stream_event", "parent_tool_use_id": nil,
			"event": map[string]any{"type": "content_block_delta", "index": 0, "delta": map[string]any{"type": "text_delta", "text": p}}})
	}
	line(map[string]any{"type": "assistant", "message": map[string]any{"content": []any{}}})
	line(map[string]any{"type": "result", "subtype": "success", "is_error": false, "result": "Answer: " + q,
		"stop_reason": "end_turn", "session_id": sid,
		"usage": map[string]any{"input_tokens": 2, "output_tokens": 5, "cache_read_input_tokens": 3, "cache_creation_input_tokens": 1}})
}

// fakeCommand prints its arguments after the first and what it read, in
// small pieces (a two-byte character split across writes).
func fakeCommand() {
	in, _ := io.ReadAll(os.Stdin)
	if string(in) == "fail" {
		fmt.Fprintln(os.Stderr, "bad question")
		os.Exit(2)
	}
	out := []byte("args=" + strings.Join(os.Args[1:], "|") + " in=" + string(in) + " é")
	for i := range out {
		os.Stdout.Write(out[i : i+1])
	}
}

func TestClaudeCode(t *testing.T) {
	t.Setenv("QA_FAKE", "claude")
	argsFile := filepath.Join(t.TempDir(), "args")
	t.Setenv("QA_FAKE_ARGS", argsFile)
	cc := ClaudeCode{Program: os.Args[0], Dir: filepath.Join(t.TempDir(), "work")}
	var got []string
	t1 := cc.Ask(context.Background(), Conversation{}, "Hi?", func(s string) { got = append(got, s) })
	if t1.Error != "" || t1.Answer != "Answer: Hi?" || strings.Join(got, "|") != "An|swer: Hi?" {
		t.Fatalf("turn %+v, deltas %q", t1, got)
	}
	if t1.Engine != EngineClaude || t1.Model != "claude-fake-1" || t1.Session == "" || len(t1.Raw) != 0 ||
		t1.Usage != (Usage{Input: 2, Output: 5, CacheRead: 3, CacheWrite: 1}) {
		t.Errorf("turn %+v", t1)
	}
	args, _ := os.ReadFile(argsFile)
	for _, want := range []string{"-p", "stream-json", "--include-partial-messages", "--tools\n\n", "--strict-mcp-config",
		"--setting-sources\n\n", "--session-id\n" + t1.Session} {
		if !strings.Contains(string(args), want) {
			t.Errorf("args lack %q:\n%s", want, args)
		}
	}
	if strings.Contains(string(args), "--model") {
		t.Errorf("a model without one chosen:\n%s", args)
	}

	// The next question resumes the session, with the model chosen.
	cc.Model = "haiku"
	t2 := cc.Ask(context.Background(), Conversation{Turns: []Turn{t1}}, "And?", func(string) {})
	args, _ = os.ReadFile(argsFile)
	if t2.Error != "" || t2.Session != t1.Session || !strings.Contains(string(args), "--resume\n"+t1.Session) ||
		!strings.Contains(string(args), "--model\nhaiku") {
		t.Errorf("follow-up %+v:\n%s", t2, args)
	}

	if tl := cc.Ask(context.Background(), Conversation{}, "login", func(string) {}); !strings.Contains(tl.Error, "Not logged in") ||
		!strings.Contains(tl.Error, "log in") || tl.Session != "" {
		t.Errorf("not logged in: %+v", tl)
	}
	if tc := cc.Ask(context.Background(), Conversation{}, "crash", func(string) {}); !strings.Contains(tc.Error, "boom") ||
		!strings.Contains(tc.Error, "exit status 3") {
		t.Errorf("crash: %q", tc.Error)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	if ts := cc.Ask(ctx, Conversation{}, "slow", func(string) {}); ts.Error != "stopped" {
		t.Errorf("stop: %q", ts.Error)
	}
	cc.Program = filepath.Join(t.TempDir(), "no-claude")
	if tn := cc.Ask(context.Background(), Conversation{}, "x", func(string) {}); !strings.Contains(tn.Error, "not found") {
		t.Errorf("missing program: %q", tn.Error)
	}
}

func TestCommand(t *testing.T) {
	t.Setenv("QA_FAKE", "cmd")
	dir := filepath.Join(t.TempDir(), "work")
	cm := Command{Name: "echo", Argv: []string{os.Args[0], "-m", "{model}"}, Model: "m1", Dir: dir}
	var got strings.Builder
	t1 := cm.Ask(context.Background(), Conversation{}, "Hi?", func(s string) { got.WriteString(s) })
	if t1.Error != "" || t1.Answer != "args=-m|m1 in=Hi? é" || got.String() != t1.Answer || t1.Engine != "cmd:echo" || t1.Model != "m1" {
		t.Fatalf("turn %+v, streamed %q", t1, got.String())
	}
	// A command keeps no conversation: the earlier turns come with the question.
	c := Conversation{Turns: []Turn{t1, {Question: "lost", Error: "stopped"}}}
	cm.Argv = []string{os.Args[0], "{question}"}
	t2 := cm.Ask(context.Background(), c, "And?", func(string) {})
	if want := "args=" + Transcript(c, "And?") + " in= é"; t2.Answer != want || !strings.Contains(t2.Answer, "Question:\nHi?") ||
		strings.Contains(t2.Answer, "lost") {
		t.Errorf("transcript: %q", t2.Answer)
	}
	cm.Argv = []string{os.Args[0]}
	if tf := cm.Ask(context.Background(), Conversation{}, "fail", func(string) {}); !strings.Contains(tf.Error, "bad question") {
		t.Errorf("failure: %q", tf.Error)
	}
	if tm := (Command{Name: "x", Argv: []string{os.Args[0], "{model}"}, Dir: dir}).Ask(context.Background(), Conversation{}, "q", func(string) {}); !strings.Contains(tm.Error, "lists no models") {
		t.Errorf("no model: %q", tm.Error)
	}
	if te := (Command{Name: "x", Dir: dir}).Ask(context.Background(), Conversation{}, "q", func(string) {}); !strings.Contains(te.Error, "no program") {
		t.Errorf("no program: %q", te.Error)
	}
}

func TestCompleteUTF8(t *testing.T) {
	e := []byte("aé")
	for _, c := range []struct {
		b    []byte
		want int
	}{{e, 3}, {e[:2], 1}, {[]byte("a"), 1}, {nil, 0}, {[]byte("€")[:2], 0}} {
		if got := completeUTF8(c.b); got != c.want {
			t.Errorf("completeUTF8(%q) = %d, want %d", c.b, got, c.want)
		}
	}
}

func TestModels(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/models" {
			t.Errorf("path %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"data":[{"id":"claude-opus-5-5","display_name":"Claude Opus 5.5","lifecycle":"active","type":"model"},`+
			`{"id":"claude-old","display_name":"Old","lifecycle":"retired","type":"model"},`+
			`{"id":"claude-haiku-5-5","display_name":"Claude Haiku 5.5","lifecycle":"deprecated","type":"model"}],"has_more":false}`)
	}))
	defer srv.Close()
	ms, err := Asker{Options: []option.RequestOption{option.WithBaseURL(srv.URL), option.WithAPIKey("test")}}.Models(context.Background())
	if err != nil || len(ms) != 2 || ms[0] != (Model{"claude-opus-5-5", "Claude Opus 5.5"}) || ms[1].ID != "claude-haiku-5-5" {
		t.Errorf("models %+v, %v", ms, err)
	}
}

func TestEngineName(t *testing.T) {
	for id, want := range map[string]string{"": "Claude API", "api": "Claude API", "claude": "Claude Code", "cmd:gemini": "gemini"} {
		if got := EngineName(id); got != want {
			t.Errorf("EngineName(%q) = %q", id, got)
		}
	}
}
