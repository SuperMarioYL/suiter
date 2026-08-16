package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/spf13/viper"

	"github.com/SuperMarioYL/suiter/internal/llm"
	"github.com/SuperMarioYL/suiter/internal/suite"
)

// agentFeishu is a fake feishu Suite that returns a fixed doc body on Read and
// records nothing — used to prove the agent loop's read stage.
type agentFeishu struct{ docBody []byte }

func (f *agentFeishu) Name() string { return "feishu" }
func (f *agentFeishu) Login(context.Context) (suite.Token, error) {
	return suite.Token{}, errors.New("unused")
}
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

func (d *agentDingtalk) Name() string { return "dingtalk" }
func (d *agentDingtalk) Login(context.Context) (suite.Token, error) {
	return suite.Token{}, errors.New("unused")
}
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

// TestCalendarEventFromSummary_UTF8Truncation (fix-calendar-title-utf8-truncation)
// proves a long summary — especially a Chinese one (~3 bytes/rune) — is
// truncated to <=80 runes AND stays valid UTF-8. Before the fix, title[:80]
// sliced mid-rune → invalid UTF-8 → json.Marshal emitted U+FFFD mojibake in the
// 钉钉 calendar event title.
func TestCalendarEventFromSummary_UTF8Truncation(t *testing.T) {
	cases := []struct {
		name    string
		summary string
	}{
		{"ascii-over-80-runes", strings.Repeat("a", 200)},
		{"chinese-over-80-runes", strings.Repeat("钉", 100)}, // 100 runes = 300 bytes; old title[:80] hit mid-rune at byte 80 (rune 27)
		{"chinese-mixed", "会议纪要：" + strings.Repeat("项目进展", 50)},
		{"under-limit", strings.Repeat("钉", 40)}, // 40 runes, must pass through unchanged
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			body, err := calendarEventFromSummary(tc.summary)
			if err != nil {
				t.Fatalf("calendarEventFromSummary: %v", err)
			}
			// The whole JSON body must be valid UTF-8 (no U+FFFD substitution).
			if !utf8.Valid(body) {
				t.Fatalf("event body is not valid UTF-8: %q", body)
			}
			var evt map[string]any
			if err := json.Unmarshal(body, &evt); err != nil {
				t.Fatalf("unmarshal event: %v (body=%s)", err, body)
			}
			title, _ := evt["summary"].(string)
			if !utf8.ValidString(title) {
				t.Fatalf("title not valid UTF-8: %q", title)
			}
			if strings.Contains(title, "\ufffd") { // U+FFFD replacement char from mid-rune slicing
				t.Fatalf("title contains U+FFFD mojibake from mid-rune slicing: %q", title)
			}
			if rc := utf8.RuneCountInString(title); rc > 80 {
				t.Fatalf("title rune count = %d, want <= 80", rc)
			}
		})
	}
}

// TestTruncate_RuneSafe (fix-calendar-title-utf8-truncation) proves truncate
// never splits a multibyte rune and that the ellipsis still appends as one rune.
func TestTruncate_RuneSafe(t *testing.T) {
	s := strings.Repeat("钉", 10) // 10 runes, 30 bytes
	got := truncate(s, 5)
	if !utf8.ValidString(got) {
		t.Fatalf("truncate produced invalid UTF-8: %q", got)
	}
	// 5 truncated runes + "…" (one rune) = 6 runes total.
	if rc := utf8.RuneCountInString(got); rc != 6 {
		t.Fatalf("truncate rune count = %d, want 6 (5 + …)", rc)
	}
	if !strings.HasSuffix(got, "…") {
		t.Fatalf("truncate = %q, want … suffix", got)
	}

	// ASCII path must behave identically to the old byte-based implementation.
	if got := truncate("hello world", 5); got != "hello…" {
		t.Fatalf("truncate(hello world,5) = %q, want hello…", got)
	}
}

// TestNewLLMClientFromConfig_DefaultModelByProvider (feat-llm-default-model-by-provider)
// proves newLLMClientFromConfig defaults the model from the RESOLVED base URL
// when SUITER_LLM_MODEL is unset — GLMBaseURL → glm-4.6, DeepSeekBaseURL →
// deepseek-chat — so `suiter agent run summarize-and-schedule` works with only
// SUITER_LLM_API_KEY set (the minimal setup the README advertises; the m3
// star-moment used to fail opaquely at the LLM layer sending model:""). The
// function's own doc comment claims "defaulting to GLM-4.6" yet before this it
// passed viper.GetString("llm_model") (== "" when unset) through verbatim. An
// explicit SUITER_LLM_MODEL always wins. Defaulting BY provider (not a single
// global default) avoids sending glm-4.6 to DeepSeek, which would regress the
// DeepSeek path.
func TestNewLLMClientFromConfig_DefaultModelByProvider(t *testing.T) {
	t.Cleanup(viper.Reset)
	cases := []struct {
		name       string
		baseURLEnv string // value for SUITER_LLM_BASE_URL ("" => default to GLM)
		modelEnv   string // value for SUITER_LLM_MODEL ("" => default by provider)
		want       string
	}{
		{"glm_default", "", "", llm.GLMDefaultModel},
		{"deepseek_default", llm.DeepSeekBaseURL, "", llm.DeepSeekDefaultModel},
		{"explicit_model_wins", "", "my-model", "my-model"},
		{"explicit_model_wins_over_deepseek", llm.DeepSeekBaseURL, "my-model", "my-model"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			setupViperForTest(t)
			t.Setenv("SUITER_LLM_API_KEY", "test-key")
			t.Setenv("SUITER_LLM_BASE_URL", tc.baseURLEnv)
			t.Setenv("SUITER_LLM_MODEL", tc.modelEnv)

			got := newLLMClientFromConfig().Model()
			if got != tc.want {
				t.Fatalf("model = %q, want %q (provider-based model defaulting broken)", got, tc.want)
			}
		})
	}
}
