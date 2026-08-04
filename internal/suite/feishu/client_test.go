package feishu

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/SuperMarioYL/suiter/internal/suite"
)

// newTestClient builds a Client whose baseURL points at the given server (so
// appAccessToken + exchange hit the test server). lark is nil — these paths
// never touch the SDK client.
func newTestClient(baseURL string) *Client {
	return &Client{appID: "app", appSecret: "secret", baseURL: baseURL}
}

// feishuMux serves the app_access_token + oidc access_token endpoints from a
// single mux, and records the last oidc request body/headers for assertion.
func feishuMux(t *testing.T, oidcResp string) (*httptest.Server, *string, *string) {
	t.Helper()
	var gotBody, gotCT string
	mux := http.NewServeMux()
	mux.HandleFunc("/open-apis/auth/v3/app_access_token/internal", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"code":0,"msg":"ok","app_access_token":"app-tok"}`))
	})
	mux.HandleFunc("/open-apis/authen/v1/oidc/access_token", func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		gotCT = r.Header.Get("Content-Type")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(oidcResp))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv, &gotBody, &gotCT
}

// TestExchange_ParsesNestedData proves fix-feishu-oidc-token-parse: the token
// fields live under a top-level "data" object. Reading them at the top level
// left AccessToken "" (the m1 break); the fix reads tr.Data.*.
func TestExchange_ParsesNestedData(t *testing.T) {
	oidcResp := `{"code":0,"msg":"ok","data":{"access_token":"u-at","refresh_token":"u-rt","token_type":"Bearer","expires_in":7200,"refresh_expires_in":2592000}}`
	srv, _, _ := feishuMux(t, oidcResp)
	c := newTestClient(srv.URL)

	tok, err := c.exchange(context.Background(), "the-code")
	if err != nil {
		t.Fatalf("exchange: %v", err)
	}
	if tok.AccessToken != "u-at" {
		t.Fatalf("access_token = %q, want u-at (nested data parse)", tok.AccessToken)
	}
	if tok.RefreshToken != "u-rt" {
		t.Fatalf("refresh_token = %q, want u-rt", tok.RefreshToken)
	}
	if tok.ExpiresIn != 7200 {
		t.Fatalf("expires_in = %d, want 7200", tok.ExpiresIn)
	}
	if tok.TokenType != "Bearer" {
		t.Fatalf("token_type = %q, want Bearer", tok.TokenType)
	}
	if tok.ObtainedAt == 0 {
		t.Fatal("obtained_at not set")
	}
}

// TestExchange_JSONBody proves the other half of the fix: the request is a
// JSON body (matching the official larksuite SDK contract), NOT form-encoded.
func TestExchange_JSONBody(t *testing.T) {
	oidcResp := `{"code":0,"msg":"ok","data":{"access_token":"u-at","expires_in":7200}}`
	srv, gotBody, gotCT := feishuMux(t, oidcResp)
	c := newTestClient(srv.URL)

	if _, err := c.exchange(context.Background(), "the-code"); err != nil {
		t.Fatalf("exchange: %v", err)
	}
	if !strings.HasPrefix(*gotCT, "application/json") {
		t.Fatalf("Content-Type = %q, want application/json (was form-encoded before the fix)", *gotCT)
	}
	var got struct {
		GrantType string `json:"grant_type"`
		Code      string `json:"code"`
	}
	if err := json.Unmarshal([]byte(*gotBody), &got); err != nil {
		t.Fatalf("body not JSON: %v (body=%s)", err, *gotBody)
	}
	if got.GrantType != "authorization_code" || got.Code != "the-code" {
		t.Fatalf("body = %s, want grant_type=authorization_code code=the-code", *gotBody)
	}
}

// TestExchange_TopLevelCodeGuard keeps the code/msg guard working: a non-zero
// code still errors (success vs failure is gated on the top-level code, not
// on data presence).
func TestExchange_TopLevelCodeGuard(t *testing.T) {
	oidcResp := `{"code":10003,"msg":"invalid code","data":{}}`
	srv, _, _ := feishuMux(t, oidcResp)
	c := newTestClient(srv.URL)

	if _, err := c.exchange(context.Background(), "bad"); err == nil {
		t.Fatal("exchange: want error on non-zero code, got nil")
	}
}

// TestCachedToken_Expiry proves fix-feishu-cached-token-expiry: a token past
// its ExpiresIn is rejected locally instead of being sent to die at 401.
func TestCachedToken_Expiry(t *testing.T) {
	now := time.Now().Unix()
	cases := []struct {
		name string
		tok  suite.Token
		want string // "" => want success; otherwise substring of the error
	}{
		{"empty", suite.Token{AccessToken: "", ExpiresIn: 7200, ObtainedAt: now}, "empty cached token"},
		{"fresh", suite.Token{AccessToken: "at", ExpiresIn: 7200, ObtainedAt: now}, ""},
		{"expired", suite.Token{AccessToken: "at", ExpiresIn: 60, ObtainedAt: now - 120}, "token expired"},
		{"no-expiry", suite.Token{AccessToken: "at", ExpiresIn: 0, ObtainedAt: now - 999999}, ""}, // ExpiresIn==0 => no staleness guard
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tok := tc.tok
			c := &Client{tokenGetter: func(context.Context) (suite.Token, error) { return tok, nil }}
			got, err := c.cachedToken(context.Background())
			if tc.want == "" {
				if err != nil {
					t.Fatalf("cachedToken: want nil err, got %v", err)
				}
				if got != tok.AccessToken {
					t.Fatalf("cachedToken = %q, want %q", got, tok.AccessToken)
				}
				return
			}
			if err == nil {
				t.Fatalf("cachedToken: want err containing %q, got nil", tc.want)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("cachedToken err = %q, want substring %q", err.Error(), tc.want)
			}
		})
	}
}

// TestCachedToken_NoGetter proves the wiring guard.
func TestCachedToken_NoGetter(t *testing.T) {
	c := &Client{}
	if _, err := c.cachedToken(context.Background()); err == nil {
		t.Fatal("want error when no token getter wired")
	}
}

// --- OAuth loopback handler (fix-feishu-oauth-loopback-hang) ---

func do(t *testing.T, baseURL, path string) *http.Response {
	t.Helper()
	resp, err := http.Get(baseURL + path)
	if err != nil {
		t.Fatalf("GET %s: %v", path, err)
	}
	return resp
}

// TestOAuthHandler_FaviconDoesNotAbort proves a stray /favicon.ico (the
// classic deadlock/abort trigger) is 404'd without touching the channels,
// so the real callback still wins.
func TestOAuthHandler_FaviconDoesNotAbort(t *testing.T) {
	state := "st"
	codeCh := make(chan string, 1)
	errCh := make(chan error, 1)
	srv := httptest.NewServer(newTestClient("").oauthHandler(state, codeCh, errCh))
	t.Cleanup(srv.Close)

	// favicon first — must NOT abort
	if resp := do(t, srv.URL, "/favicon.ico"); resp.StatusCode != http.StatusNotFound {
		t.Fatalf("favicon status = %d, want 404 (no channel touch)", resp.StatusCode)
	} else {
		resp.Body.Close()
	}
	select {
	case e := <-errCh:
		t.Fatalf("favicon pushed a spurious error, aborting login: %v", e)
	case <-codeCh:
		t.Fatal("favicon should not have produced a code")
	default:
	}

	// then the real callback wins
	if resp := do(t, srv.URL, "/callback?state=st&code=win"); resp.StatusCode != http.StatusOK {
		t.Fatalf("callback status = %d, want 200", resp.StatusCode)
	} else {
		resp.Body.Close()
	}
	select {
	case code := <-codeCh:
		if code != "win" {
			t.Fatalf("codeCh = %q, want win", code)
		}
	case <-time.After(time.Second):
		t.Fatal("real callback did not deliver code")
	}
	select {
	case e := <-errCh:
		t.Fatalf("errCh should be empty after happy callback, got %v", e)
	default:
	}
}

// TestOAuthHandler_StateMismatchNoAbort proves a /callback with the wrong
// state is 400'd WITHOUT pushing to errCh (so a replay/second connection
// cannot abort the flow). The real callback still wins afterwards.
func TestOAuthHandler_StateMismatchNoAbort(t *testing.T) {
	state := "st"
	codeCh := make(chan string, 1)
	errCh := make(chan error, 1)
	srv := httptest.NewServer(newTestClient("").oauthHandler(state, codeCh, errCh))
	t.Cleanup(srv.Close)

	if resp := do(t, srv.URL, "/callback?state=wrong&code=x"); resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("mismatched-state status = %d, want 400", resp.StatusCode)
	} else {
		resp.Body.Close()
	}
	select {
	case e := <-errCh:
		t.Fatalf("state mismatch pushed a spurious error: %v", e)
	default:
	}

	if resp := do(t, srv.URL, "/callback?state=st&code=win"); resp.StatusCode != http.StatusOK {
		t.Fatalf("callback status = %d, want 200", resp.StatusCode)
	} else {
		resp.Body.Close()
	}
	select {
	case code := <-codeCh:
		if code != "win" {
			t.Fatalf("codeCh = %q, want win", code)
		}
	case <-time.After(time.Second):
		t.Fatal("real callback did not deliver code after a stray mismatch")
	}
}

// TestOAuthHandler_MissingCodeResponds400 proves the `code == ""` branch no
// longer silently returns 200 with no signal (it now responds 400).
func TestOAuthHandler_MissingCodeResponds400(t *testing.T) {
	state := "st"
	codeCh := make(chan string, 1)
	errCh := make(chan error, 1)
	srv := httptest.NewServer(newTestClient("").oauthHandler(state, codeCh, errCh))
	t.Cleanup(srv.Close)

	resp := do(t, srv.URL, "/callback?state=st")
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("missing-code status = %d, want 400 (was silent 200 before the fix)", resp.StatusCode)
	}
	resp.Body.Close()
	select {
	case <-codeCh:
		t.Fatal("missing-code should not deliver a code")
	case e := <-errCh:
		t.Fatalf("missing-code should not touch errCh, got %v", e)
	default:
	}
}

// TestOAuthHandler_ReloadDoesNotDeadlock proves a second /callback (reload)
// finds the codeCh buffer full and returns without blocking — the handler
// goroutine always returns, so Shutdown cannot deadlock.
func TestOAuthHandler_ReloadDoesNotDeadlock(t *testing.T) {
	state := "st"
	codeCh := make(chan string, 1)
	errCh := make(chan error, 1)
	srv := httptest.NewServer(newTestClient("").oauthHandler(state, codeCh, errCh))
	t.Cleanup(srv.Close)

	if resp := do(t, srv.URL, "/callback?state=st&code=first"); resp.StatusCode != http.StatusOK {
		t.Fatalf("first callback status = %d, want 200", resp.StatusCode)
	} else {
		resp.Body.Close()
	}

	// second callback (reload) — must return promptly without blocking
	done := make(chan struct{})
	go func() {
		resp := do(t, srv.URL, "/callback?state=st&code=second")
		resp.Body.Close()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("second callback (reload) blocked — Shutdown would deadlock (regression of the fix)")
	}

	// only the first code wins
	if code := <-codeCh; code != "first" {
		t.Fatalf("codeCh = %q, want first", code)
	}
}

// TestOAuthHandler_ErrorParamAborts proves a genuine provider denial (error
// query param, with the right state) still aborts via errCh — the fix keeps
// the real-denial path, only stray/replay requests are silenced.
func TestOAuthHandler_ErrorParamAborts(t *testing.T) {
	state := "st"
	codeCh := make(chan string, 1)
	errCh := make(chan error, 1)
	srv := httptest.NewServer(newTestClient("").oauthHandler(state, codeCh, errCh))
	t.Cleanup(srv.Close)

	resp := do(t, srv.URL, "/callback?state=st&error=access_denied")
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("error-param status = %d, want 400", resp.StatusCode)
	}
	resp.Body.Close()
	select {
	case e := <-errCh:
		if !strings.Contains(e.Error(), "access_denied") {
			t.Fatalf("errCh = %q, want access_denied", e.Error())
		}
	case <-time.After(time.Second):
		t.Fatal("error param should have aborted via errCh")
	}
}

// TestOAuthHandler_NonCallback404 proves paths other than /callback are 404
// (so probes/second connections cannot reach channel logic).
func TestOAuthHandler_NonCallback404(t *testing.T) {
	state := "st"
	codeCh := make(chan string, 1)
	errCh := make(chan error, 1)
	srv := httptest.NewServer(newTestClient("").oauthHandler(state, codeCh, errCh))
	t.Cleanup(srv.Close)

	for _, p := range []string{"/", "/robots.txt", "/something"} {
		resp := do(t, srv.URL, p)
		if resp.StatusCode != http.StatusNotFound {
			t.Fatalf("GET %s status = %d, want 404", p, resp.StatusCode)
		}
		resp.Body.Close()
	}
}

// TestNewClient_Defaults proves the public constructor keeps the contract.
func TestNewClient_Defaults(t *testing.T) {
	c := NewClient("id", "secret")
	if c.appID != "id" || c.appSecret != "secret" {
		t.Fatal("credentials not set")
	}
	if c.baseURL != DefaultBaseURL {
		t.Fatalf("baseURL = %q, want %q", c.baseURL, DefaultBaseURL)
	}
	if c.Name() != "feishu" {
		t.Fatalf("Name = %q, want feishu", c.Name())
	}
}

// Ensure bytes is used (the JSON body fix) — keeps imports honest if the
// above tests are trimmed later.
var _ = bytes.NewReader
