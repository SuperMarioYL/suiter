package tencentdocs

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"golang.org/x/oauth2"

	"github.com/SuperMarioYL/suiter/internal/suite"
)

// newTestClient wires a Client whose token endpoint + api base point at the
// given test servers (so exchange + sheet calls are fully drivable).
func newTestClient(tokenURL, apiBase string) *Client {
	return &Client{
		clientID:     "cid",
		clientSecret: "csec",
		apiBase:      apiBase,
		oauth: &oauth2.Config{
			ClientID:     "cid",
			ClientSecret: "csec",
			Endpoint:     oauth2.Endpoint{AuthURL: "https://example.invalid/auth", TokenURL: tokenURL},
		},
	}
}

func withToken(c *Client, tok suite.Token) *Client {
	c.tokenGetter = func(context.Context) (suite.Token, error) { return tok, nil }
	return c
}

// TestExchange_StandardOAuth proves m3 tencentdocs exchange handles 腾讯文档's
// STANDARD OAuth2 contract (JSON body grant_type/code/client_id/client_secret;
// snake_case response access_token/refresh_token/token_type/expires_in) —
// distinct from DingTalk's non-standard camelCase.
func TestExchange_StandardOAuth(t *testing.T) {
	var gotBody string
	var gotCT string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		gotCT = r.Header.Get("Content-Type")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"u-at","refresh_token":"u-rt","token_type":"Bearer","expires_in":7200}`))
	}))
	t.Cleanup(srv.Close)
	c := newTestClient(srv.URL, "https://example.invalid")

	tok, err := c.exchange(context.Background(), "the-code")
	if err != nil {
		t.Fatalf("exchange: %v", err)
	}
	if tok.AccessToken != "u-at" {
		t.Fatalf("access_token = %q, want u-at", tok.AccessToken)
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

	// request contract: standard OAuth2 JSON body
	if !strings.HasPrefix(gotCT, "application/json") {
		t.Fatalf("Content-Type = %q, want application/json", gotCT)
	}
	var got struct {
		GrantType    string `json:"grant_type"`
		Code         string `json:"code"`
		ClientID     string `json:"client_id"`
		ClientSecret string `json:"client_secret"`
	}
	if err := json.Unmarshal([]byte(gotBody), &got); err != nil {
		t.Fatalf("body not JSON: %v (body=%s)", err, gotBody)
	}
	if got.GrantType != "authorization_code" || got.Code != "the-code" || got.ClientID != "cid" || got.ClientSecret != "csec" {
		t.Fatalf("body = %s, want grant_type=authorization_code code=the-code client_id=cid client_secret=csec", gotBody)
	}
}

// TestExchange_NoToken proves a response without an access token errors.
func TestExchange_NoToken(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"access_token":""}`))
	}))
	t.Cleanup(srv.Close)
	c := newTestClient(srv.URL, "https://example.invalid")
	if _, err := c.exchange(context.Background(), "c"); err == nil {
		t.Fatal("want error when no access token returned")
	}
}

// sheetMux serves the sheet read + write endpoints and records the token + path.
func sheetMux(t *testing.T, writeID string) (*httptest.Server, *string, map[string]string) {
	t.Helper()
	var gotTok string
	got := make(map[string]string)
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/spreadsheets/", func(w http.ResponseWriter, r *http.Request) {
		got["path"] = r.URL.Path
		got["method"] = r.Method
		gotTok = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodPost {
			_, _ = w.Write([]byte(`{"id":"` + writeID + `","range":"A1:B2"}`))
			return
		}
		_, _ = w.Write([]byte(`{"spreadsheet":{"id":"sh-1"},"sheets":[{"id":"s1","title":"Sheet1"}]}`))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv, &gotTok, got
}

// TestRead_Sheet proves Read("sheet", id) routes to GET .../spreadsheets/<id>
// with the cached access token (Bearer) and returns the raw body.
func TestRead_Sheet(t *testing.T) {
	srv, gotTok, got := sheetMux(t, "")
	c := withToken(newTestClient("https://example.invalid/token", srv.URL), suite.Token{AccessToken: "u-at"})

	body, err := c.Read(context.Background(), "sheet", "sh-1")
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if !strings.Contains(string(body), "sh-1") {
		t.Fatalf("body = %s, want spreadsheet sh-1", string(body))
	}
	if *gotTok != "Bearer u-at" {
		t.Fatalf("Authorization = %q, want Bearer u-at", *gotTok)
	}
	if got["method"] != http.MethodGet {
		t.Fatalf("read method = %q, want GET", got["method"])
	}
	if !strings.HasSuffix(got["path"], "/spreadsheets/sh-1") {
		t.Fatalf("read path = %q, want suffix /spreadsheets/sh-1", got["path"])
	}
}

// TestWrite_Sheet proves Write("sheet", id, body) routes to POST
// .../spreadsheets/<id>/values with the JSON body and returns an id.
func TestWrite_Sheet(t *testing.T) {
	srv, gotTok, got := sheetMux(t, "rng-7")
	c := withToken(newTestClient("https://example.invalid/token", srv.URL), suite.Token{AccessToken: "u-at"})

	cellJSON := []byte(`{"range":"A1","values":[["hello"]]}}`)
	id, err := c.Write(context.Background(), "sheet", "sh-1", cellJSON)
	if err != nil {
		t.Fatalf("Write: %v", err)
	}
	if id != "rng-7" {
		t.Fatalf("write id = %q, want rng-7", id)
	}
	if *gotTok != "Bearer u-at" {
		t.Fatalf("Authorization = %q, want Bearer u-at", *gotTok)
	}
	if got["method"] != http.MethodPost {
		t.Fatalf("write method = %q, want POST", got["method"])
	}
	if !strings.HasSuffix(got["path"], "/spreadsheets/sh-1/values") {
		t.Fatalf("write path = %q, want suffix /spreadsheets/sh-1/values", got["path"])
	}
}

// sheetWriteMux serves a fixed write response body so the write-path
// error-envelope / fake-success tests can drive sheetWrite directly.
func sheetWriteMux(t *testing.T, writeBody string) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/spreadsheets/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(writeBody))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

// TestWrite_Sheet_ErrorEnvelope (fix-dingtalk-tencentdocs-write-fake-success)
// proves sheetWrite surfaces a Tencent-Docs HTTP-200 error envelope instead of
// masking it with the hard-coded "written" id. Before the fix, a 200-body
// lacking `id`/`range` made Write return "written" and the agent loop exited 0,
// silently losing the written cells.
func TestWrite_Sheet_ErrorEnvelope(t *testing.T) {
	srv := sheetWriteMux(t, `{"code":1,"errmsg":"no permission to write"}`)
	c := withToken(newTestClient("https://example.invalid/token", srv.URL), suite.Token{AccessToken: "u-at"})

	id, err := c.Write(context.Background(), "sheet", "sh-1", []byte(`{"range":"A1"}`))
	if err == nil {
		t.Fatalf("Write: want error on HTTP-200 error envelope, got id=%q (the fake \"written\" id — the bug)", id)
	}
	if !strings.Contains(err.Error(), "no permission") {
		t.Fatalf("Write err = %q, want the API errmsg", err.Error())
	}
}

// TestWrite_Sheet_NoFakeIDOnWrongField proves a 200 success whose id lives
// under a non-`id`/`range` field surfaces the raw body instead of "written"
// (fix-dingtalk-tencentdocs-write-fake-success).
func TestWrite_Sheet_NoFakeIDOnWrongField(t *testing.T) {
	srv := sheetWriteMux(t, `{"updatedRange":"A1:B2","updatedCells":4}`)
	c := withToken(newTestClient("https://example.invalid/token", srv.URL), suite.Token{AccessToken: "u-at"})

	id, err := c.Write(context.Background(), "sheet", "sh-1", []byte(`{"range":"A1"}`))
	if err == nil {
		t.Fatalf("Write: want error surfacing the renamed-field body, got id=%q (the fake \"written\" id — the bug)", id)
	}
	if !strings.Contains(err.Error(), "updatedRange") {
		t.Fatalf("Write err = %q, want the raw body (with updatedRange) surfaced so the field mismatch is observable", err.Error())
	}
}

// TestWrite_Sheet_RealIDStillReturned proves a genuine success carrying `id`
// still returns it (the fix did not break the happy path).
func TestWrite_Sheet_RealIDStillReturned(t *testing.T) {
	srv := sheetWriteMux(t, `{"id":"rng-42","range":"A1:B2"}`)
	c := withToken(newTestClient("https://example.invalid/token", srv.URL), suite.Token{AccessToken: "u-at"})

	id, err := c.Write(context.Background(), "sheet", "sh-1", []byte(`{"range":"A1"}`))
	if err != nil {
		t.Fatalf("Write: %v (genuine success with id must not error)", err)
	}
	if id != "rng-42" {
		t.Fatalf("write id = %q, want rng-42", id)
	}
}

// sheetReadMux serves a fixed read response body (any path/method) so the
// read-path error-envelope test can drive sheetRead directly.
func sheetReadMux(t *testing.T, readBody string) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(readBody))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

// TestRead_Sheet_ErrorEnvelope (fix-dingtalk-tencentdocs-read-error-envelope)
// proves sheetRead surfaces a Tencent-Docs HTTP-200 error envelope instead of
// returning it as the sheet content. Before the fix, doRaw's 200 check passed
// and the error JSON was handed to the agent/user as if it were the sheet —
// the same silent-failure class fixed for the write path (sheetWrite) in v0.6.0.
func TestRead_Sheet_ErrorEnvelope(t *testing.T) {
	srv := sheetReadMux(t, `{"code":1,"errmsg":"no permission to read sheet"}`)
	c := withToken(newTestClient("https://example.invalid/token", srv.URL), suite.Token{AccessToken: "u-at"})

	body, err := c.Read(context.Background(), "sheet", "sh-1")
	if err == nil {
		t.Fatalf("Read: want error on HTTP-200 error envelope, got body=%s (the error JSON returned as the sheet — the bug)", string(body))
	}
	if !strings.Contains(err.Error(), "no permission") {
		t.Fatalf("Read err = %q, want the API errmsg", err.Error())
	}
}

// TestRead_Sheet_SuccessUnchanged proves the guard is behavior-preserving for a
// real 200 success carrying no envelope fields (the sheet body is returned
// verbatim, not rejected as a parse/envelope error).
func TestRead_Sheet_SuccessUnchanged(t *testing.T) {
	srv := sheetReadMux(t, `{"spreadsheet":{"id":"sh-1"},"sheets":[{"id":"s1"}]}`)
	c := withToken(newTestClient("https://example.invalid/token", srv.URL), suite.Token{AccessToken: "u-at"})

	body, err := c.Read(context.Background(), "sheet", "sh-1")
	if err != nil {
		t.Fatalf("Read: want nil err on plain success, got %v", err)
	}
	if !strings.Contains(string(body), "sh-1") {
		t.Fatalf("body = %s, want spreadsheet sh-1", string(body))
	}
}

// TestRead_RequiresID proves an empty sheet id errors before the call.
func TestRead_RequiresID(t *testing.T) {
	c := withToken(&Client{}, suite.Token{AccessToken: "at"})
	if _, err := c.Read(context.Background(), "sheet", ""); err == nil {
		t.Fatal("want error when sheet read has no id")
	}
}

// TestRead_UnimplementedKind proves unknown kinds error (not a stub panicking).
func TestRead_UnimplementedKind(t *testing.T) {
	c := withToken(&Client{}, suite.Token{AccessToken: "at"})
	if _, err := c.Read(context.Background(), "doc", "x"); err == nil {
		t.Fatal("want error for unimplemented kind")
	}
}

// TestCachedToken_GuardsAndExpiry proves the cached-token guards (mirror the
// other suites) AND the staleness guard shipped in v0.3.0: a token past its
// ExpiresIn is rejected locally instead of being sent to die at the API.
func TestCachedToken_GuardsAndExpiry(t *testing.T) {
	c := &Client{} // no getter
	if _, err := c.cachedToken(context.Background()); err == nil {
		t.Fatal("want error when no token getter wired")
	}
	c2 := withToken(&Client{}, suite.Token{AccessToken: ""})
	if _, err := c2.cachedToken(context.Background()); err == nil {
		t.Fatal("want error when cached token is empty")
	}

	now := time.Now().Unix()
	cases := []struct {
		name string
		tok  suite.Token
		want string // "" => want success; otherwise substring of the error
	}{
		{"fresh", suite.Token{AccessToken: "at", ExpiresIn: 7200, ObtainedAt: now}, ""},
		{"expired", suite.Token{AccessToken: "at", ExpiresIn: 60, ObtainedAt: now - 120}, "token expired"},
		{"no-expiry", suite.Token{AccessToken: "at", ExpiresIn: 0, ObtainedAt: now - 999999}, ""}, // ExpiresIn==0 => no staleness guard
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cc := withToken(&Client{}, tc.tok)
			got, err := cc.cachedToken(context.Background())
			if tc.want == "" {
				if err != nil {
					t.Fatalf("cachedToken: want nil err, got %v", err)
				}
				if got != tc.tok.AccessToken {
					t.Fatalf("cachedToken = %q, want %q", got, tc.tok.AccessToken)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("cachedToken err = %v, want substring %q", err, tc.want)
			}
		})
	}
}

// TestNewClient_Defaults proves the public constructor keeps the contract.
func TestNewClient_Defaults(t *testing.T) {
	c := NewClient("cid", "csec")
	if c.clientID != "cid" || c.clientSecret != "csec" {
		t.Fatal("credentials not set")
	}
	if c.apiBase != defaultAPIBase {
		t.Fatalf("apiBase = %q, want %q", c.apiBase, defaultAPIBase)
	}
	if c.oauth.Endpoint.TokenURL != "https://open.tencent.com/oauth2/token" {
		t.Fatalf("token URL = %q", c.oauth.Endpoint.TokenURL)
	}
	if c.Name() != Name || c.Name() != "tencentdocs" {
		t.Fatalf("Name = %q, want tencentdocs", c.Name())
	}
}
