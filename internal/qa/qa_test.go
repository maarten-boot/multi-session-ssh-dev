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
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/anthropics/anthropic-sdk-go/v2/option"
)

// sse is a streamed answer: text in pieces, then the stop reason.
func sse(w io.Writer, stop string, pieces ...string) {
	ev := func(name, data string) { fmt.Fprintf(w, "event: %s\ndata: %s\n\n", name, data) }
	ev("message_start", `{"type":"message_start","message":{"id":"msg_1","type":"message","role":"assistant",`+
		`"model":"claude-opus-5-5","content":[],"stop_reason":null,"stop_sequence":null,`+
		`"usage":{"input_tokens":10,"output_tokens":1,"cache_read_input_tokens":4,"cache_creation_input_tokens":0}}}`)
	ev("content_block_start", `{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`)
	for _, p := range pieces {
		b, _ := json.Marshal(p)
		ev("content_block_delta", `{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":`+string(b)+`}}`)
	}
	ev("content_block_stop", `{"type":"content_block_stop","index":0}`)
	extra := ""
	if stop == "refusal" {
		extra = `,"stop_details":{"type":"refusal","category":"cyber","explanation":"not this"}`
	}
	ev("message_delta", `{"type":"message_delta","delta":{"stop_reason":"`+stop+`","stop_sequence":null`+extra+`},"usage":{"output_tokens":7}}`)
	ev("message_stop", `{"type":"message_stop"}`)
}

type request struct {
	Model     string            `json:"model"`
	Fallbacks string            `json:"fallbacks"`
	Messages  []json.RawMessage `json:"messages"`
	System    []struct{ Text string }
	Output    struct{ Effort string } `json:"output_config"`
	Cache     *struct{ Type string }  `json:"cache_control"`
}

// fake is a Claude API that answers every question with pieces and stop,
// recording the requests.
func fake(t *testing.T, stop string, pieces ...string) (*httptest.Server, *[]request, *[]http.Header) {
	var reqs []request
	var heads []http.Header
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var q request
		if err := json.NewDecoder(r.Body).Decode(&q); err != nil {
			t.Errorf("request body: %v", err)
		}
		reqs = append(reqs, q)
		heads = append(heads, r.Header.Clone())
		w.Header().Set("Content-Type", "text/event-stream")
		sse(w, stop, pieces...)
	}))
	t.Cleanup(srv.Close)
	return srv, &reqs, &heads
}

func asker(srv *httptest.Server) Asker {
	return Asker{Options: []option.RequestOption{option.WithBaseURL(srv.URL), option.WithAPIKey("test")}}
}

func TestAskStreamsAndContinues(t *testing.T) {
	srv, reqs, heads := fake(t, "end_turn", "Hel", "lo")
	a := asker(srv)
	var got []string
	t1 := a.Ask(context.Background(), Conversation{}, "Hi?", func(s string) { got = append(got, s) })
	if t1.Error != "" || t1.Answer != "Hello" || strings.Join(got, "|") != "Hel|lo" {
		t.Fatalf("turn %+v, deltas %q", t1, got)
	}
	if t1.Usage != (Usage{Input: 10, Output: 7, CacheRead: 4}) || t1.Model != "claude-opus-5-5" || len(t1.Raw) == 0 {
		t.Errorf("usage/model/raw: %+v %q %d", t1.Usage, t1.Model, len(t1.Raw))
	}
	q := (*reqs)[0]
	if q.Model != DefaultModel || q.Fallbacks != "default" || len(q.Messages) != 1 || q.Output.Effort != "medium" ||
		q.Cache == nil || q.Cache.Type != "ephemeral" || len(q.System) != 1 {
		t.Errorf("first request: %+v", q)
	}
	if b := (*heads)[0].Get("anthropic-beta"); !strings.Contains(b, "server-side-fallback-2026-07-01") {
		t.Errorf("beta header %q", b)
	}

	// The follow-up sends the first question and answer back, then the new one.
	c := Conversation{Turns: []Turn{t1}}
	t2 := Asker{Model: "claude-sonnet-5-5", Options: a.Options}.Ask(context.Background(), c, "And?", func(string) {})
	if t2.Error != "" {
		t.Fatal(t2.Error)
	}
	q = (*reqs)[1]
	if q.Model != "claude-sonnet-5-5" || len(q.Messages) != 3 {
		t.Fatalf("second request: model %q, %d messages", q.Model, len(q.Messages))
	}
	if !strings.Contains(string(q.Messages[1]), `"role":"assistant"`) || !strings.Contains(string(q.Messages[1]), "Hello") ||
		!strings.Contains(string(q.Messages[2]), "And?") {
		t.Errorf("messages: %s", q.Messages)
	}
}

func TestAskRefusalAndErrors(t *testing.T) {
	srv, _, _ := fake(t, "refusal", "partial")
	tr := asker(srv).Ask(context.Background(), Conversation{}, "x", func(string) {})
	if !strings.Contains(tr.Error, "declined") || !strings.Contains(tr.Error, "not this") || len(tr.Raw) != 0 {
		t.Errorf("refusal turn: %+v", tr)
	}
	// A refused turn is not sent back.
	ms, err := Messages(Conversation{Turns: []Turn{tr}}, "y")
	if err != nil || len(ms) != 1 {
		t.Errorf("messages after a refusal: %d, %v", len(ms), err)
	}

	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(401)
		io.WriteString(w, `{"type":"error","error":{"type":"authentication_error","message":"invalid x-api-key"}}`)
	}))
	defer bad.Close()
	te := Asker{Options: []option.RequestOption{option.WithBaseURL(bad.URL), option.WithAPIKey("test"), option.WithMaxRetries(0)}}.
		Ask(context.Background(), Conversation{}, "x", func(string) {})
	if !strings.Contains(te.Error, "ANTHROPIC_API_KEY") {
		t.Errorf("401 error: %q", te.Error)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	tc := asker(srv).Ask(ctx, Conversation{}, "x", func(string) {})
	if tc.Error != "stopped" || len(tc.Raw) != 0 {
		t.Errorf("cancelled: %+v", tc)
	}
}

func TestStore(t *testing.T) {
	s := Store{Dir: filepath.Join(t.TempDir(), "qa")}
	if c, err := s.Load("s-1"); err != nil || len(c.Turns) != 0 {
		t.Fatalf("missing: %+v %v", c, err)
	}
	c := Conversation{Turns: []Turn{{Time: time.Unix(0, 0).UTC(), Question: "q", Answer: "a", Raw: json.RawMessage(`{"id":"m"}`)}}}
	if err := s.Save("s-1", c); err != nil {
		t.Fatal(err)
	}
	got, err := s.Load("s-1")
	if err != nil || len(got.Turns) != 1 || got.Turns[0].Answer != "a" || string(got.Turns[0].Raw) != `{"id":"m"}` {
		t.Fatalf("round trip: %+v %v", got, err)
	}
	if fi, _ := os.Stat(filepath.Join(s.Dir, "s-1.json")); runtime.GOOS != "windows" && fi.Mode().Perm() != 0o600 {
		t.Errorf("mode %v", fi.Mode().Perm())
	}
	for _, id := range []string{"", "../x", `a\b`, ".hidden"} {
		if _, err := s.Load(id); err == nil {
			t.Errorf("id %q accepted", id)
		}
	}
	if err := s.Clear("s-1"); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.Load("s-1"); len(got.Turns) != 0 {
		t.Error("not cleared")
	}
	if err := s.Clear("s-1"); err != nil {
		t.Errorf("clear twice: %v", err)
	}
	os.WriteFile(filepath.Join(s.Dir, "bad.json"), []byte("{"), 0o600)
	if _, err := s.Load("bad"); err == nil {
		t.Error("broken file loaded")
	}
}

func TestLog(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "logs", "host", "path")
	when := time.Date(2026, 10, 30, 9, 5, 0, 0, time.Local)
	Log(dir, "alpha", Turn{Time: when, Question: "Q1", Answer: "A1", Model: "m", Usage: Usage{Input: 3, CacheRead: 2, Output: 5}})
	Log(dir, "alpha", Turn{Time: when.Add(time.Minute), Question: "Q2", Error: "stopped"})
	b, err := os.ReadFile(filepath.Join(dir, "qa-2026-10-30.md"))
	if err != nil {
		t.Fatal(err)
	}
	s := strings.ReplaceAll(string(b), "\r\n", "\n")
	for _, want := range []string{"## 2026-10-30 09:05:00 — alpha", "Q1", "**Answer** (m):\n\nA1", "Tokens: 5 in (2 from cache), 5 out",
		"## 2026-10-30 09:06:00 — alpha", "**Error:** stopped"} {
		if !strings.Contains(s, want) {
			t.Errorf("log lacks %q:\n%s", want, s)
		}
	}
}
