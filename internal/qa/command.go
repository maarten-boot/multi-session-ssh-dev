package qa

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"
	"unicode/utf8"
)

// MaxAnswer is the most of a command's output taken as its answer.
const MaxAnswer = 4 << 20

// Command answers through another program (a [[ui.qa_commands]] entry):
// Argv is the program and its arguments, where {model} is replaced by the
// model and {question} by the question with the conversation before it;
// without a {question} argument that text goes to its standard input. Its
// standard output, as it comes, is the answer.
type Command struct {
	Name  string
	Argv  []string
	Model string
	Dir   string
}

// Transcript is the question with the earlier answered questions before
// it, for a program that keeps no conversation of its own.
func Transcript(c Conversation, question string) string {
	var b strings.Builder
	for _, t := range c.Turns {
		if t.Answer == "" {
			continue
		}
		if b.Len() == 0 {
			b.WriteString("Earlier in this conversation:\n\n")
		}
		fmt.Fprintf(&b, "Question:\n%s\n\nAnswer:\n%s\n\n", t.Question, t.Answer)
	}
	if b.Len() == 0 {
		return question
	}
	fmt.Fprintf(&b, "Now the question:\n%s", question)
	return b.String()
}

func (cm Command) Ask(ctx context.Context, c Conversation, question string, delta func(string)) Turn {
	t := Turn{Time: time.Now(), Question: question, Engine: CommandPrefix + cm.Name, Model: cm.Model}
	if len(cm.Argv) == 0 || strings.TrimSpace(cm.Argv[0]) == "" {
		t.Error = fmt.Sprintf("the Q&A command %q has no program (command = [...] in the config)", cm.Name)
		return t
	}
	text := Transcript(c, question)
	args, viaArg := make([]string, len(cm.Argv)), false
	for i, a := range cm.Argv {
		if strings.Contains(a, "{model}") && cm.Model == "" {
			t.Error = fmt.Sprintf("the Q&A command %q uses {model} but lists no models", cm.Name)
			return t
		}
		viaArg = viaArg || strings.Contains(a, "{question}")
		args[i] = strings.NewReplacer("{model}", cm.Model, "{question}", text).Replace(a)
	}
	if err := os.MkdirAll(cm.Dir, 0o700); err != nil {
		t.Error = err.Error()
		return t
	}
	cmd := exec.CommandContext(ctx, args[0], args[1:]...)
	cmd.Dir = cm.Dir
	if !viaArg {
		cmd.Stdin = strings.NewReader(text)
	}
	var stderr tail
	cmd.Stderr = &stderr
	Configure(cmd)
	out, err := cmd.StdoutPipe()
	if err != nil {
		t.Error = err.Error()
		return t
	}
	if err := cmd.Start(); err != nil {
		t.Error = fmt.Sprintf("%s did not start: %v", cm.Name, err)
		return t
	}
	var answer strings.Builder
	var pending []byte // an incomplete UTF-8 character at the end of a read
	buf := make([]byte, 32*1024)
	cut := false
	for {
		n, rerr := out.Read(buf)
		pending = append(pending, buf[:n]...)
		if k := completeUTF8(pending); k > 0 && !cut {
			s := string(pending[:k])
			if answer.Len()+len(s) > MaxAnswer {
				s, cut = s[:0], true
				cmd.Cancel()
			}
			answer.WriteString(s)
			delta(s)
			pending = append(pending[:0], pending[k:]...)
		}
		if rerr != nil {
			break
		}
	}
	io.Copy(io.Discard, out)
	werr := cmd.Wait()
	t.Answer = answer.String() + string(pending)
	switch {
	case cut:
		t.Error = "the answer was cut off at 4 MB"
	case ctx.Err() != nil:
		t.Error = "stopped"
	case werr != nil:
		t.Error = fmt.Sprintf("%s ended (%v)", cm.Name, werr)
		if s := stderr.lastLine(); s != "" {
			t.Error += ": " + s
		}
	case t.Answer == "":
		t.Error = cm.Name + " gave no answer"
		if s := stderr.lastLine(); s != "" {
			t.Error += ": " + s
		}
	}
	return t
}

// completeUTF8 is the length of b without an incomplete character at its
// end.
func completeUTF8(b []byte) int {
	for i := len(b) - 1; i >= 0 && i >= len(b)-utf8.UTFMax; i-- {
		if utf8.RuneStart(b[i]) {
			if utf8.FullRune(b[i:]) {
				return len(b)
			}
			return i
		}
	}
	return len(b)
}
