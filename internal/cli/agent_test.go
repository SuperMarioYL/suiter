package cli

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/SuperMarioYL/suiter/internal/suite"
)

// agentFeishu is a fake feishu Suite that returns a fixed doc body on Read and
// records nothing — used to prove the agent loop's read stage.
type agentFeishu struct{ docBody []byte }

func (f *agentFeishu) Name() string                                   { return "feishu" }
func (f *agentFeishu) Login(context.Context) (suite.Token, error)     { return suite.Token{}, errors.New("unused") }
func (f *agentFeishu) Read(_ context.Context, _, _ string) ([]byte, error) { return f.docBody, nil }
func (f *agentFeishu) Write(context.Context, string, string, []byte) (string, error) {
	return "", errors.New("feishu write unused")
}

// agentDingtalk is a fake dingtalk Suite that records the calendar Write body
// and returns a fixed event id — used to prove the agent loop's write stage.
type agentDingtalk struct {
	gotBody []byte
	gotKind string
}

func (d *agentDingtalk) Name() string                               { return "dingtalk" }
func (d *agentDingtalk) Login(context.Context) (suite.Token, error) { return suite.Token{}, errors.New("unused") }
func (d *agentDingtalk) Read(context.Context, string, string) ([]byte, error) {
	return nil, errors.New("dingtalk read unused")
}
func (d *agentDingtalk) Write(_ context.Context, kind, _ string, body []byte) (string, error) {
	d.gotKind = kind
	d.gotBody = body
	return "evt-42", nil
}

// fakeSummarizer returns a fixed summary, recording the content it received.
type fakeSummarizer struct {
	gotContent string
	out        string
	err        error
}

func (s *fakeSummarizer) Summarize(_ context.Context, content string) (string, error) {
	s.gotContent = content
	return s.out, s.err
}

// TestSummarizeAndSchedule_EndToEnd proves the m3 star-moment loop wiring:
// read 飞书 doc → summarize → write 钉钉 calendar event, with the summary
// landing in the event body and the event id surfacing on stdout.
func TestSummarizeAndSchedule_EndToEnd(t *testing.T) {
	reg := suite.NewRegistry()
	feishu := &agentFeishu{docBody: []byte(`{"doc":{"title":"Q3 plan","body":"ship m3..."}}`)}
	dingtalk := &agentDingtalk{}
	reg.Register(feishu)
	reg.Register(dingtalk)

	sm := &fakeSummarizer{out: "Q3 plan: ship m3 this week; owner: yulei"}
	var out bytes.Buffer

	if err := summarizeAndSchedule(context.Background(), reg, sm, "doc-7", &out); err != nil {
		t.Fatalf("summarizeAndSchedule: %v", err)
	}

	// feishu was read for the right doc id
	if !strings.Contains(string(feishu.docBody), "ship m3") {
		t.Fatalf("feishu doc body unexpected: %s", feishu.docBody)
	}

	// summarizer received the doc body content
	if !strings.Contains(sm.gotContent, "ship m3") {
		t.Fatalf("summarizer got %q, want the doc body content", sm.gotContent)
	}

	// dingtalk calendar Write received the summary in the event JSON
	if dingtalk.gotKind != "calendar" {
		t.Fatalf("dingtalk write kind = %q, want calendar", dingtalk.gotKind)
	}
	if !strings.Contains(string(dingtalk.gotBody), "Q3 plan: ship m3") {
		t.Fatalf("calendar event body = %s, want it to carry the summary", dingtalk.gotBody)
	}

	// event id surfaced on stdout
	if !strings.Contains(out.String(), "evt-42") {
		t.Fatalf("stdout = %q, want event id evt-42", out.String())
	}
}

// TestSummarizeAndSchedule_MissingSuite proves a missing feishu/dingtalk suite
// errors clearly (no nil-map panic).
func TestSummarizeAndSchedule_MissingSuite(t *testing.T) {
	reg := suite.NewRegistry() // empty
	sm := &fakeSummarizer{out: "x"}
	if err := summarizeAndSchedule(context.Background(), reg, sm, "doc-1", &bytes.Buffer{}); err == nil {
		t.Fatal("want error when feishu suite not registered")
	}
}

// TestSummarizeAndSchedule_SummarizeError proves an LLM error surfaces.
func TestSummarizeAndSchedule_SummarizeError(t *testing.T) {
	reg := suite.NewRegistry()
	reg.Register(&agentFeishu{docBody: []byte("doc")})
	reg.Register(&agentDingtalk{})
	sm := &fakeSummarizer{err: errors.New("glm 429 too many requests")}
	err := summarizeAndSchedule(context.Background(), reg, sm, "doc-1", &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "429") {
		t.Fatalf("err = %v, want 429 surfaced", err)
	}
}

// TestCalendarEventFromSummary proves the event JSON carries the summary as
// title + description and has start/end dates.
func TestCalendarEventFromSummary(t *testing.T) {
	body, err := calendarEventFromSummary("meeting notes: ship m3")
	if err != nil {
		t.Fatalf("calendarEventFromSummary: %v", err)
	}
	s := string(body)
	for _, want := range []string{"ship m3", "start", "end", "summary", "description"} {
		if !strings.Contains(s, want) {
			t.Fatalf("event body = %s, want substring %q", s, want)
		}
	}
}

// TestTruncate proves the helper.
func TestTruncate(t *testing.T) {
	if got := truncate("hello", 10); got != "hello" {
		t.Fatalf("truncate(hello,10) = %q, want hello", got)
	}
	if got := truncate("hello world", 5); !strings.HasPrefix(got, "hello") {
		t.Fatalf("truncate(hello world,5) = %q, want hello…", got)
	}
}
