// Package qa is the Q&A tab: questions to Claude through the Claude API,
// one saved conversation per session, and a log of every question and
// answer. Credentials come from the environment (ANTHROPIC_API_KEY) or an
// `ant auth login` profile, as the SDK finds them; the app stores none.
package qa

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/anthropics/anthropic-sdk-go/v2"
	"github.com/anthropics/anthropic-sdk-go/v2/option"
)

// DefaultModel is used when the config names none.
const DefaultModel = "claude-opus-5-5"

// system tells Claude how its answers are shown.
const system = "You answer questions in the Q&A tab of Session Workbench, a desktop app for " +
	"working in coding sessions on remote hosts. Answers are shown as plain text: " +
	"use Markdown sparingly, put code in fenced code blocks with a language, " +
	"and when a picture helps, draw it as SVG in an ```svg block."

// Usage is the token count of one answer.
type Usage struct {
	Input      int64 `json:"input"`
	Output     int64 `json:"output"`
	CacheRead  int64 `json:"cache_read"`
	CacheWrite int64 `json:"cache_write"`
}

// Turn is one question with its answer. Raw is the API's message, sent
// back unchanged with the next question (it carries the thinking blocks);
// a turn without one (an error, a refusal, a stopped answer) is not.
type Turn struct {
	Time     time.Time       `json:"time"`
	Question string          `json:"question"`
	Answer   string          `json:"answer"`
	Model    string          `json:"model,omitempty"`
	Usage    Usage           `json:"usage"`
	Error    string          `json:"error,omitempty"`
	Raw      json.RawMessage `json:"raw,omitempty"`
}

// Conversation is a session's Q&A.
type Conversation struct {
	Turns []Turn `json:"turns"`
}

// Store keeps each session's conversation in Dir/<session id>.json.
type Store struct {
	Dir string
}

func (s Store) path(id string) (string, error) {
	if s.Dir == "" {
		return "", errors.New("no data directory for the Q&A tab")
	}
	if id == "" || strings.ContainsAny(id, `/\:`) || strings.HasPrefix(id, ".") {
		return "", fmt.Errorf("invalid session id %q", id)
	}
	return filepath.Join(s.Dir, id+".json"), nil
}

// Load returns the session's conversation (empty when there is none).
func (s Store) Load(id string) (Conversation, error) {
	var c Conversation
	p, err := s.path(id)
	if err != nil {
		return c, err
	}
	b, err := os.ReadFile(p)
	if errors.Is(err, os.ErrNotExist) {
		return c, nil
	}
	if err != nil {
		return c, err
	}
	if err := json.Unmarshal(b, &c); err != nil {
		return Conversation{}, fmt.Errorf("%s: %w", p, err)
	}
	return c, nil
}

// Save writes the conversation atomically, readable only by the user.
func (s Store) Save(id string, c Conversation) error {
	p, err := s.path(id)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(s.Dir, 0o700); err != nil {
		return err
	}
	b, err := json.Marshal(c)
	if err != nil {
		return err
	}
	tmp := p + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, p)
}

// Clear removes the session's conversation (its log stays).
func (s Store) Clear(id string) error {
	p, err := s.path(id)
	if err != nil {
		return err
	}
	if err := os.Remove(p); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

// Log appends a turn to dir/qa-YYYY-MM-DD.md (the day of the question).
func Log(dir, session string, t Turn) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	f, err := os.OpenFile(filepath.Join(dir, "qa-"+t.Time.Format("2006-01-02")+".md"),
		os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	var b strings.Builder
	fmt.Fprintf(&b, "## %s — %s\n\n**Question:**\n\n%s\n\n", t.Time.Format("2006-01-02 15:04:05"), session, t.Question)
	if t.Answer != "" {
		fmt.Fprintf(&b, "**Answer** (%s):\n\n%s\n\n", t.Model, t.Answer)
	}
	if t.Error != "" {
		fmt.Fprintf(&b, "**Error:** %s\n\n", t.Error)
	}
	fmt.Fprintf(&b, "Tokens: %d in (%d from cache), %d out\n\n", t.Usage.Input+t.Usage.CacheRead+t.Usage.CacheWrite,
		t.Usage.CacheRead, t.Usage.Output)
	_, err = f.WriteString(b.String())
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	return err
}

// Messages is the conversation as API messages: the earlier turns that
// have an answer to send back, then the new question.
func Messages(c Conversation, question string) ([]anthropic.BetaMessageParam, error) {
	var ms []anthropic.BetaMessageParam
	for _, t := range c.Turns {
		if len(t.Raw) == 0 {
			continue
		}
		var m anthropic.BetaMessage
		if err := json.Unmarshal(t.Raw, &m); err != nil {
			return nil, fmt.Errorf("saved answer of %s: %w", t.Time.Format(time.DateTime), err)
		}
		ms = append(ms, anthropic.NewBetaUserMessage(anthropic.NewBetaTextBlock(t.Question)), m.ToParam())
	}
	return append(ms, anthropic.NewBetaUserMessage(anthropic.NewBetaTextBlock(question))), nil
}

// Asker sends questions to Claude.
type Asker struct {
	Model   string
	Options []option.RequestOption // for tests: base URL, key
}

// Ask streams the answer to question after the conversation c, calling
// delta with each piece of text as it arrives. The turn it returns has the
// whole answer, or the error with the text that came before it.
func (a Asker) Ask(ctx context.Context, c Conversation, question string, delta func(string)) Turn {
	model := a.Model
	if model == "" {
		model = DefaultModel
	}
	t := Turn{Time: time.Now(), Question: question, Model: model}
	msgs, err := Messages(c, question)
	if err != nil {
		t.Error = err.Error()
		return t
	}
	client := anthropic.NewClient(a.Options...)
	params := anthropic.BetaMessageNewParams{
		Model:     anthropic.Model(model),
		MaxTokens: 64000,
		System:    []anthropic.BetaTextBlockParam{{Text: system}},
		Messages:  msgs,
		// Effort: the default of the current Opus, set so it stays.
		OutputConfig: anthropic.BetaOutputConfigParam{Effort: anthropic.BetaOutputConfigEffortMedium},
		// Cache the conversation so far: a follow-up question pays less.
		CacheControl: anthropic.NewBetaCacheControlEphemeralParam(),
		// A question one model declines is answered by another one.
		Fallbacks: anthropic.BetaFallbacksParamOfDefault(),
		Betas:     []anthropic.AnthropicBeta{anthropic.AnthropicBetaServerSideFallback2026_07_01},
	}
	stream := client.Beta.Messages.NewStreaming(ctx, params)
	var m anthropic.BetaMessage
	var text strings.Builder
	for stream.Next() {
		ev := stream.Current()
		if err := m.Accumulate(ev); err != nil {
			t.Error = err.Error()
			break
		}
		if d, ok := ev.AsAny().(anthropic.BetaRawContentBlockDeltaEvent); ok {
			if td, ok := d.Delta.AsAny().(anthropic.BetaTextDelta); ok {
				text.WriteString(td.Text)
				delta(td.Text)
			}
		}
	}
	t.Answer = text.String()
	t.Usage = Usage{Input: m.Usage.InputTokens, Output: m.Usage.OutputTokens,
		CacheRead: m.Usage.CacheReadInputTokens, CacheWrite: m.Usage.CacheCreationInputTokens}
	if m.Model != "" {
		t.Model = string(m.Model)
	}
	if err := stream.Err(); err != nil {
		t.Error = describe(err)
		return t
	}
	if t.Error != "" {
		return t
	}
	switch m.StopReason {
	case anthropic.BetaStopReasonRefusal:
		t.Error = "Claude declined to answer"
		if m.StopDetails.Explanation != "" {
			t.Error += ": " + m.StopDetails.Explanation
		}
		return t
	case anthropic.BetaStopReasonMaxTokens:
		t.Error = "the answer was cut off at the length limit"
	}
	t.Raw = json.RawMessage(m.RawJSON())
	return t
}

// describe turns an API error into a sentence for the tab.
func describe(err error) string {
	if errors.Is(err, context.Canceled) {
		return "stopped"
	}
	var apierr *anthropic.Error
	if errors.As(err, &apierr) {
		switch apierr.StatusCode {
		case 401, 403:
			return "the Claude API refused the credentials: set ANTHROPIC_API_KEY or run `ant auth login`, then restart the app"
		case 429:
			return "rate limited by the Claude API; ask again in a moment"
		}
		return fmt.Sprintf("Claude API error %d: %s", apierr.StatusCode, apierr.Error())
	}
	msg := err.Error()
	if strings.Contains(msg, "API key") || strings.Contains(msg, "credential") || strings.Contains(msg, "auth") {
		return "no Claude API credentials: set ANTHROPIC_API_KEY or run `ant auth login`, then restart the app (" + msg + ")"
	}
	return msg
}
