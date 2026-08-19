package suite

import (
	"testing"
	"time"
)

// TestHTTPClient_NonZeroTimeout (fix-suite-http-client-no-timeout) proves the
// shared suite HTTP client has a non-zero Timeout so a stalled endpoint fails
// within ~30s instead of hanging the CLI forever. All four suite clients
// (feishu/dingtalk/wework/tencentdocs) call this in place of
// http.DefaultClient (which has Timeout==0), so this single assertion covers
// every suite call site. Mirrors the LLM client's 60s timeout
// (internal/llm/summarize.go) but tighter — suite reads are small JSON
// bodies, not model inference.
func TestHTTPClient_NonZeroTimeout(t *testing.T) {
	c := HTTPClient()
	if c == nil {
		t.Fatal("HTTPClient() returned nil")
	}
	if c.Timeout <= 0 {
		t.Fatalf("HTTPClient().Timeout = %v, want > 0 (http.DefaultClient has Timeout==0 and hangs the CLI on a stalled endpoint)", c.Timeout)
	}
	// the suite deadline must mirror the LLM client's intent: a sane bound that
	// fails a black-holed read within the agent-loop window, not 0 (forever).
	if c.Timeout != suiteHTTPTimeout {
		t.Fatalf("HTTPClient().Timeout = %v, want %v", c.Timeout, suiteHTTPTimeout)
	}
	if c.Timeout > 60*time.Second {
		t.Fatalf("HTTPClient().Timeout = %v, want <= 60s (suite calls are not model inference)", c.Timeout)
	}
}
