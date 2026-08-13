// Package cli — agent loops. The m3 star-moment: `suiter agent run
// summarize-and-schedule <feishu-doc-id>` wires the end-to-end loop
// read 飞书 doc → GLM/DeepSeek summarize → write 钉钉 calendar event, all
// behind the Suite interface + shared TokenStore + the OpenAI-compatible LLM
// client. No per-suite glue, no model hosting.
package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/SuperMarioYL/suiter/internal/llm"
	"github.com/SuperMarioYL/suiter/internal/suite"
)

// summarizer is the subset of the LLM client the agent loop needs. Declared as
// an interface so the loop is unit-testable with a fake summarizer (no live
// GLM/DeepSeek call required to prove the read→summarize→write wiring).
type summarizer interface {
	Summarize(ctx context.Context, content string) (string, error)
}

// newAgentCommand builds the `suiter agent run <loop>` tree. m3 ships the
// summarize-and-schedule loop for real (no "lands in m3" stub).
func newAgentCommand(reg *suite.Registry) *cobra.Command {
	agentCmd := &cobra.Command{
		Use:   "agent",
		Short: "Agent loops",
	}
	runCmd := &cobra.Command{
		Use:   "run",
		Short: "Run an agent loop",
	}
	sumSchedCmd := &cobra.Command{
		Use:   "summarize-and-schedule <feishu-doc-id>",
		Short: "Read a 飞书 doc → GLM/DeepSeek summarize → 钉钉 calendar event",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			llmClient := newLLMClientFromConfig()
			out := cmd.OutOrStdout()
			return summarizeAndSchedule(cmd.Context(), reg, llmClient, args[0], out)
		},
	}
	runCmd.AddCommand(sumSchedCmd)
	agentCmd.AddCommand(runCmd)
	return agentCmd
}

// newLLMClientFromConfig wires the OpenAI-compatible GLM/DeepSeek client from
// viper env (SUITER_LLM_BASE_URL / SUITER_LLM_API_KEY / SUITER_LLM_MODEL),
// defaulting to GLM-4.6. Returns nil if no API key is set; the loop errors
// clearly in that case rather than failing at the HTTP layer.
func newLLMClientFromConfig() *llm.Client {
	baseURL := viper.GetString("llm_base_url")
	if baseURL == "" {
		baseURL = llm.GLMBaseURL
	}
	return llm.NewClient(baseURL, viper.GetString("llm_api_key"), viper.GetString("llm_model"))
}

// summarizeAndSchedule is the end-to-end m3 star-moment loop, extracted so it
// is unit-testable with a fake summarizer + fake suites:
//  1. feishu.Read("doc", docID) → agent-readable doc body
//  2. llm.Summarize(body) → one-paragraph summary
//  3. build a 钉钉 calendar event from the summary + write it via
//     dingtalk.Write("calendar", "primary", event)
//
// It prints the created event id (or {"id":...} JSON via the caller's --json
// flag handled by the generic dispatcher, not here). Errors at each stage
// surface clearly so a coding agent can act on them.
func summarizeAndSchedule(ctx context.Context, reg *suite.Registry, sm summarizer, feishuDocID string, w io.Writer) error {
	feishu, ok := reg.Get("feishu")
	if !ok {
		return fmt.Errorf("agent: feishu suite not registered")
	}
	dingtalk, ok := reg.Get("dingtalk")
	if !ok {
		return fmt.Errorf("agent: dingtalk suite not registered")
	}

	docBody, err := feishu.Read(ctx, "doc", feishuDocID)
	if err != nil {
		return fmt.Errorf("agent: read feishu doc %q: %w", feishuDocID, err)
	}

	summary, err := sm.Summarize(ctx, string(docBody))
	if err != nil {
		return fmt.Errorf("agent: summarize: %w", err)
	}

	event, err := calendarEventFromSummary(summary)
	if err != nil {
		return fmt.Errorf("agent: build calendar event: %w", err)
	}

	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	eventID, err := dingtalk.Write(ctx, "calendar", "primary", event)
	if err != nil {
		return fmt.Errorf("agent: write dingtalk calendar: %w", err)
	}

	fmt.Fprintf(w, "scheduled: %s\n  doc:      %s\n  summary:  %s\n  event:    %s\n",
		eventID, feishuDocID, truncate(summary, 80), eventID)
	return nil
}

// calendarEventFromSummary builds a minimal 钉钉 calendar event JSON from a
// summary: the summary is the event title, the start is now+1h, end now+2h.
// 钉钉's calendar create accepts a summary + start/end; a coding agent can
// edit the resulting event for specifics. This is the demo-loop shape, not a
// full scheduler.
func calendarEventFromSummary(summary string) ([]byte, error) {
	now := time.Now()
	title := summary
	// fix-calendar-title-utf8-truncation: truncate by rune count, not bytes.
	// A Chinese summary is ~3 bytes/rune, so the old title[:80] byte slice
	// landed mid-rune → invalid UTF-8 → json.Marshal emitted U+FFFD mojibake
	// in the 钉钉 calendar event title. Rune-safe truncation stays valid UTF-8.
	if r := []rune(title); len(r) > 80 {
		title = string(r[:80])
	}
	evt := map[string]any{
		"summary":     title,
		"start":       map[string]string{"date": now.Add(1 * time.Hour).Format("2006-01-02 15:04:05")},
		"end":         map[string]string{"date": now.Add(2 * time.Hour).Format("2006-01-02 15:04:05")},
		"description": summary,
	}
	return json.Marshal(evt)
}

func truncate(s string, n int) string {
	s = strings.TrimSpace(s)
	// fix-calendar-title-utf8-truncation: rune-safe so a multibyte summary is
	// never split mid-rune (the stdout `summary:` line would otherwise mojibake).
	if len([]rune(s)) <= n {
		return s
	}
	return string([]rune(s)[:n]) + "…"
}
