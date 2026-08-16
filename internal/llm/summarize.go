// Package llm is a thin OpenAI-compatible HTTP client for CN-hosted models
// (GLM-4.6 / DeepSeek). No model hosting — just a chat-completions call.
// The end-to-end agent loop (read 飞书 → summarize → write 钉钉) is wired in m3.
package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// Default endpoints for the supported model_targets.
const (
	GLMBaseURL      = "https://open.bigmodel.cn/api/paas/v4"
	DeepSeekBaseURL = "https://api.deepseek.com"

	// GLMDefaultModel is the default model when SUITER_LLM_MODEL is unset and
	// the resolved base URL is GLMBaseURL (provider-based defaulting in
	// newLLMClientFromConfig). Matches the plan's model_target zhipu/glm-4.6.
	GLMDefaultModel = "glm-4.6"
	// DeepSeekDefaultModel is the default model when SUITER_LLM_MODEL is unset
	// and the resolved base URL is DeepSeekBaseURL. Matches deepseek/deepseek-chat.
	DeepSeekDefaultModel = "deepseek-chat"
)

// Client is an OpenAI-compatible chat client.
type Client struct {
	baseURL string
	apiKey  string
	model   string
	http    *http.Client
}

// NewClient constructs an LLM client. baseURL is the provider's OpenAI-compatible
// root (e.g. GLMBaseURL for GLM-4.6, DeepSeekBaseURL for deepseek-chat).
func NewClient(baseURL, apiKey, model string) *Client {
	return &Client{
		baseURL: baseURL,
		apiKey:  apiKey,
		model:   model,
		http:    &http.Client{Timeout: 60 * time.Second},
	}
}

// Model returns the resolved model identifier. Used by the CLI layer's tests to
// assert provider-based model defaulting (feat-llm-default-model-by-provider)
// without making a live HTTP call.
func (c *Client) Model() string { return c.model }

// Summarize returns a one-paragraph summary of content via chat completions.
func (c *Client) Summarize(ctx context.Context, content string) (string, error) {
	if c.apiKey == "" {
		return "", fmt.Errorf("llm: API key not set")
	}
	payload := map[string]any{
		"model": c.model,
		"messages": []map[string]string{
			{"role": "system", "content": "你是中文办公套件摘要助手。用一段话概括以下内容，保留关键决策、时间、负责人。"},
			{"role": "user", "content": content},
		},
		"temperature": 0.3,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("llm: marshal: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("llm: request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	resp, err := c.http.Do(req)
	if err != nil {
		return "", fmt.Errorf("llm: call: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("llm: status %d: %s", resp.StatusCode, string(raw))
	}
	var out struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return "", fmt.Errorf("llm: parse: %w", err)
	}
	if len(out.Choices) == 0 {
		return "", fmt.Errorf("llm: empty choices")
	}
	return out.Choices[0].Message.Content, nil
}
