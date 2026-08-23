package dingtalk

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
// given test servers (so exchange + calendar calls are fully drivable).
func newTestClient(tokenURL, apiBase string) *Client {
	return &Client{
		appKey:    "dk",
		appSecret: "ds",
		apiBase:   apiBase,
		oauth: &oauth2.Config{
			ClientID:     "dk",
			ClientSecret: "ds",
			Endpoint:     oauth2.Endpoint{AuthURL: "https://example.invalid/auth", TokenURL: tokenURL},
		},
	}
}

func withToken(c *Client, tok suite.Token) *Client {
	c.tokenGetter = func(context.Context) (suite.Token, error) { return tok, nil }
	return c
}

// TestExchange_ParsesCamelCase proves the m2 dingtalk exchange handles DingTalk's
// NON-standard token contract (JSON body clientId/clientSecret/grantType/code;
// camelCase response accessToken/expireIn/refreshToken) — the standard oauth2
// package cannot, so this is net/http + JSON.
func TestExchange_ParsesCamelCase(t *testing.T) {
	var gotBody string
	var gotCT string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		gotCT = r.Header.Get("Content-Type")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"accessToken":"u-at","refreshToken":"u-rt","tokenType":"DingTalk","expireIn":7200}`))
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
		t.Fatalf("expire_in = %d, want 7200", tok.ExpiresIn)
	}
	if tok.TokenType != "DingTalk" {
		t.Fatalf("token_type = %q, want DingTalk", tok.TokenType)
	}
	if tok.ObtainedAt == 0 {
		t.Fatal("obtained_at not set")
	}

	// request contract
	if !strings.HasPrefix(gotCT, "application/json") {
		t.Fatalf("Content-Type = %q, want application/json", gotCT)
	}
	var got struct {
		ClientID     string `json:"clientId"`
		ClientSecret string `json:"clientSecret"`
		GrantType    string `json:"grantType"`
		Code         string `json:"code"`
	}
	if err := json.Unmarshal([]byte(gotBody), &got); err != nil {
		t.Fatalf("body not JSON: %v (body=%s)", err, gotBody)
	}
	if got.ClientID != "dk" || got.ClientSecret != "ds" || got.GrantType != "authorization_code" || got.Code != "the-code" {
		t.Fatalf("body = %s, want clientId=dk clientSecret=ds grantType=authorization_code code=the-code", gotBody)
	}
}

// TestExchange_NoToken proves a response without an access token errors.
func TestExchange_NoToken(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"accessToken":""}`))
	}))
	t.Cleanup(srv.Close)
	c := newTestClient(srv.URL, "https://example.invalid")
	if _, err := c.exchange(context.Background(), "c"); err == nil {
		t.Fatal("want error when no access token returned")
	}
}

// calendarMux serves the calendar list/get/create endpoints and records the
// access-token header + method on each call.
func calendarMux(t *testing.T, createID string) (*httptest.Server, *string, map[string]string) {
	t.Helper()
	var gotTok string
	methods := make(map[string]string)
	mux := http.NewServeMux()
	mux.HandleFunc("/v1.0/calendar/calendars", func(w http.ResponseWriter, r *http.Request) {
		methods["list-method"] = r.Method
		gotTok = r.Header.Get("x-acs-dingtalk-access-token")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"calendars":[{"id":"cal-1"}]}`))
	})
	mux.HandleFunc("/v1.0/calendar/calendars/", func(w http.ResponseWriter, r *http.Request) {
		methods["path"] = r.URL.Path
		methods["create-method"] = r.Method
		if r.Method == http.MethodPost {
			gotTok = r.Header.Get("x-acs-dingtalk-access-token")
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"id":"` + createID + `"}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"cal-42","summary":"team"}`))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv, &gotTok, methods
}

// TestRead_CalendarList proves Read("calendar","") routes to GET .../calendars
// with the cached user access token and returns the raw body.
func TestRead_CalendarList(t *testing.T) {
	srv, gotTok, methods := calendarMux(t, "")
	c := withToken(newTestClient("https://example.invalid/token", srv.URL), suite.Token{AccessToken: "u-at"})

	body, err := c.Read(context.Background(), "calendar", "")
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if !strings.Contains(string(body), "cal-1") {
		t.Fatalf("body = %s, want calendars list", string(body))
	}
	if *gotTok != "u-at" {
		t.Fatalf("x-acs-dingtalk-access-token = %q, want u-at", *gotTok)
	}
	if methods["list-method"] != http.MethodGet {
		t.Fatalf("list method = %q, want GET", methods["list-method"])
	}
}

// TestRead_CalendarGet proves Read("calendar", id) routes to GET .../calendars/<id>.
func TestRead_CalendarGet(t *testing.T) {
	srv, _, methods := calendarMux(t, "")
	c := withToken(newTestClient("https://example.invalid/token", srv.URL), suite.Token{AccessToken: "u-at"})

	body, err := c.Read(context.Background(), "calendar", "cal-42")
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if !strings.Contains(string(body), "cal-42") {
		t.Fatalf("body = %s, want calendar cal-42", string(body))
	}
	if methods["create-method"] != http.MethodGet {
		t.Fatalf("get method = %q, want GET", methods["create-method"])
	}
}

// calendarReadMux serves a fixed read response body (any path/method) so the
// read-path error-envelope tests can drive calendarList/calendarGet directly.
func calendarReadMux(t *testing.T, readBody string) *httptest.Server {
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

// TestRead_CalendarList_ErrorEnvelope (fix-dingtalk-tencentdocs-read-error-
// envelope) proves calendarList surfaces a DingTalk HTTP-200 error envelope
// instead of returning it as the calendar list. Before the fix, doRaw's 200
// check passed and the error JSON was handed to the agent/user as if it were
// the calendar list — the same silent-failure class fixed for the write path
// (calendarCreateEvent) in v0.6.0.
func TestRead_CalendarList_ErrorEnvelope(t *testing.T) {
	srv := calendarReadMux(t, `{"code":"Forbidden","message":"no permission to list calendars"}`)
	c := withToken(newTestClient("https://example.invalid/token", srv.URL), suite.Token{AccessToken: "u-at"})

	body, err := c.Read(context.Background(), "calendar", "")
	if err == nil {
		t.Fatalf("Read: want error on HTTP-200 error envelope, got body=%s (the error JSON returned as the calendar list — the bug)", string(body))
	}
	if !strings.Contains(err.Error(), "Forbidden") && !strings.Contains(err.Error(), "no permission") {
		t.Fatalf("Read err = %q, want the API code/message", err.Error())
	}
}

// TestRead_CalendarGet_LegacyErrcodeEnvelope (fix-dingtalk-tencentdocs-read-
// error-envelope) proves a legacy cgi-bin-style errcode/errmsg 200-body on
// calendar get is surfaced, not returned as the calendar.
func TestRead_CalendarGet_LegacyErrcodeEnvelope(t *testing.T) {
	srv := calendarReadMux(t, `{"errcode":40014,"errmsg":"invalid token"}`)
	c := withToken(newTestClient("https://example.invalid/token", srv.URL), suite.Token{AccessToken: "u-at"})

	if _, err := c.Read(context.Background(), "calendar", "cal-42"); err == nil || !strings.Contains(err.Error(), "40014") {
		t.Fatalf("Read err = %v, want errcode 40014", err)
	}
}

// TestRead_CalendarList_SuccessUnchanged proves the guard is behavior-preserving
// for a real 200 success carrying no envelope fields (the calendar list is
// returned verbatim, not rejected as a parse/envelope error).
func TestRead_CalendarList_SuccessUnchanged(t *testing.T) {
	srv := calendarReadMux(t, `{"calendars":[{"id":"cal-1"}]}`)
	c := withToken(newTestClient("https://example.invalid/token", srv.URL), suite.Token{AccessToken: "u-at"})

	body, err := c.Read(context.Background(), "calendar", "")
	if err != nil {
		t.Fatalf("Read: want nil err on plain success, got %v", err)
	}
	if !strings.Contains(string(body), "cal-1") {
		t.Fatalf("body = %s, want calendars list", string(body))
	}
}

// TestWrite_CalendarCreate proves Write("calendar", ...) routes to POST
// .../calendars/primary/events with the JSON body and returns the new event id.
func TestWrite_CalendarCreate(t *testing.T) {
	srv, gotTok, methods := calendarMux(t, "evt-99")
	c := withToken(newTestClient("https://example.invalid/token", srv.URL), suite.Token{AccessToken: "u-at"})

	eventJSON := []byte(`{"summary":"weekly sync","start":{"date":"2026-08-05"}}`)
	id, err := c.Write(context.Background(), "calendar", "", eventJSON)
	if err != nil {
		t.Fatalf("Write: %v", err)
	}
	if id != "evt-99" {
		t.Fatalf("event id = %q, want evt-99", id)
	}
	if *gotTok != "u-at" {
		t.Fatalf("x-acs-dingtalk-access-token = %q, want u-at", *gotTok)
	}
	if methods["create-method"] != http.MethodPost {
		t.Fatalf("create method = %q, want POST", methods["create-method"])
	}
	if !strings.HasSuffix(methods["path"], "/primary/events") {
		t.Fatalf("create path = %q, want suffix /primary/events", methods["path"])
	}
}

// calendarCreateMux serves a fixed create response body so the write-path
// error-envelope / fake-success tests can drive calendarCreateEvent directly.
func calendarCreateMux(t *testing.T, createBody string) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/v1.0/calendar/calendars/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(createBody))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

// TestWrite_CalendarCreate_ErrorEnvelope (fix-dingtalk-tencentdocs-write-fake-
// success) proves calendarCreateEvent surfaces a DingTalk HTTP-200 error
// envelope instead of masking it with the hard-coded "created" id. Before the
// fix, a 200-body lacking `id` made Write return "created" and the agent loop
// printed `scheduled: created` and exited 0, silently losing the calendar
// event the m3 star-moment claims to create.
func TestWrite_CalendarCreate_ErrorEnvelope(t *testing.T) {
	srv := calendarCreateMux(t, `{"code":"Forbidden","message":"no permission to create event"}`)
	c := withToken(newTestClient("https://example.invalid/token", srv.URL), suite.Token{AccessToken: "u-at"})

	id, err := c.Write(context.Background(), "calendar", "primary", []byte(`{"summary":"x"}`))
	if err == nil {
		t.Fatalf("Write: want error on HTTP-200 error envelope, got id=%q (the fake \"created\" id — the bug)", id)
	}
	if !strings.Contains(err.Error(), "Forbidden") && !strings.Contains(err.Error(), "no permission") {
		t.Fatalf("Write err = %q, want the API code/message", err.Error())
	}
}

// TestWrite_CalendarCreate_NoFakeIDOnWrongField proves a 200 success whose id
// lives under a non-`id` field (e.g. eventId) surfaces the raw body
// (observable) instead of a hard-coded "created" (fix-dingtalk-tencentdocs-
// write-fake-success).
func TestWrite_CalendarCreate_NoFakeIDOnWrongField(t *testing.T) {
	srv := calendarCreateMux(t, `{"eventId":"evt-real-but-renamed"}`)
	c := withToken(newTestClient("https://example.invalid/token", srv.URL), suite.Token{AccessToken: "u-at"})

	id, err := c.Write(context.Background(), "calendar", "primary", []byte(`{"summary":"x"}`))
	if err == nil {
		t.Fatalf("Write: want error surfacing the renamed-field body, got id=%q (the fake \"created\" id — the bug)", id)
	}
	if !strings.Contains(err.Error(), "eventId") {
		t.Fatalf("Write err = %q, want the raw body (with eventId) surfaced so the field mismatch is observable", err.Error())
	}
}

// TestWrite_CalendarCreate_LegacyErrcodeEnvelope proves a legacy cgi-bin-style
// errcode/errmsg 200-body is also surfaced (not masked by "created").
func TestWrite_CalendarCreate_LegacyErrcodeEnvelope(t *testing.T) {
	srv := calendarCreateMux(t, `{"errcode":40014,"errmsg":"invalid token"}`)
	c := withToken(newTestClient("https://example.invalid/token", srv.URL), suite.Token{AccessToken: "u-at"})

	if _, err := c.Write(context.Background(), "calendar", "primary", []byte(`{"summary":"x"}`)); err == nil || !strings.Contains(err.Error(), "40014") {
		t.Fatalf("Write err = %v, want errcode 40014", err)
	}
}

// TestCachedToken_Guards proves the cached-token guards mirror feishu's.
func TestCachedToken_Guards(t *testing.T) {
	c := &Client{} // no getter
	if _, err := c.cachedToken(context.Background()); err == nil {
		t.Fatal("want error when no token getter wired")
	}
	c2 := withToken(&Client{}, suite.Token{AccessToken: ""})
	if _, err := c2.cachedToken(context.Background()); err == nil {
		t.Fatal("want error when cached token is empty")
	}
	c3 := withToken(&Client{}, suite.Token{AccessToken: "at"})
	if got, err := c3.cachedToken(context.Background()); err != nil || got != "at" {
		t.Fatalf("cachedToken = (%q,%v), want (at,nil)", got, err)
	}
}

// TestCachedToken_Expiry proves fix-dingtalk-cached-token-expiry: a token past
// its ExpiresIn is rejected locally instead of being sent to die at DingTalk
// (the same defect class fixed for feishu in v0.2.0, left un-fixed in the
// dingtalk client shipped the SAME v0.2.0 version).
func TestCachedToken_Expiry(t *testing.T) {
	now := time.Now().Unix()
	cases := []struct {
		name string
		tok  suite.Token
		want string // "" => want success; otherwise substring of the error
	}{
		{"fresh", suite.Token{AccessToken: "at", ExpiresIn: 7200, ObtainedAt: now}, ""},
		{"expired", suite.Token{AccessToken: "at", ExpiresIn: 60, ObtainedAt: now - 120}, "token expired"},
		{"no-expiry", suite.Token{AccessToken: "at", ExpiresIn: 0, ObtainedAt: now - 999999}, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := withToken(&Client{}, tc.tok)
			got, err := c.cachedToken(context.Background())
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

// TestRead_UnimplementedKind proves unknown kinds error (not a stub panicking).
func TestRead_UnimplementedKind(t *testing.T) {
	c := withToken(&Client{}, suite.Token{AccessToken: "at"})
	if _, err := c.Read(context.Background(), "doc", "x"); err == nil {
		t.Fatal("want error for unimplemented kind")
	}
}

// TestNewClient_Defaults proves the public constructor keeps the contract.
func TestNewClient_Defaults(t *testing.T) {
	c := NewClient("k", "s")
	if c.appKey != "k" || c.appSecret != "s" {
		t.Fatal("credentials not set")
	}
	if c.apiBase != defaultAPIBase {
		t.Fatalf("apiBase = %q, want %q", c.apiBase, defaultAPIBase)
	}
	if c.oauth.Endpoint.TokenURL != "https://api.dingtalk.com/v1.0/oauth2/userAccessToken" {
		t.Fatalf("token URL = %q", c.oauth.Endpoint.TokenURL)
	}
	if c.Name() != Name || c.Name() != "dingtalk" {
		t.Fatalf("Name = %q, want dingtalk", c.Name())
	}
}
