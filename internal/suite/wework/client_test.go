package wework

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"golang.org/x/oauth2"

	"github.com/SuperMarioYL/suiter/internal/suite"
)

// newTestClient wires a Client whose gettoken endpoint + api base point at
// the given test servers.
func newTestClient(tokenURL, apiBase string) *Client {
	return &Client{
		corpID:  "corp",
		agentID: "1000001",
		secret:  "sec",
		apiBase: apiBase,
		oauth: &oauth2.Config{
			ClientID:     "corp",
			ClientSecret: "sec",
			Endpoint:     oauth2.Endpoint{AuthURL: "https://example.invalid/qr", TokenURL: tokenURL},
		},
	}
}

func withToken(c *Client, tok suite.Token) *Client {
	c.tokenGetter = func(context.Context) (suite.Token, error) { return tok, nil }
	return c
}

// TestLogin_Gettoken proves m2 wework Login fetches the WeCom app access_token
// via gettoken (corpid+secret, no browser loopback) and caches it.
func TestLogin_Gettoken(t *testing.T) {
	var gotQ string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQ = r.URL.RawQuery
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"errcode":0,"errmsg":"ok","access_token":"app-tok","expires_in":7200}`))
	}))
	t.Cleanup(srv.Close)
	c := newTestClient(srv.URL, "https://example.invalid")

	tok, err := c.Login(context.Background())
	if err != nil {
		t.Fatalf("Login: %v", err)
	}
	if tok.AccessToken != "app-tok" {
		t.Fatalf("access_token = %q, want app-tok", tok.AccessToken)
	}
	if tok.ExpiresIn != 7200 {
		t.Fatalf("expires_in = %d, want 7200", tok.ExpiresIn)
	}
	if tok.ObtainedAt == 0 {
		t.Fatal("obtained_at not set")
	}
	// gettoken must carry corpid + corpsecret
	if !strings.Contains(gotQ, "corpid=corp") || !strings.Contains(gotQ, "corpsecret=sec") {
		t.Fatalf("gettoken query = %q, want corpid+corpsecret", gotQ)
	}
}

// TestLogin_Errcode proves a non-zero errcode aborts with the message.
func TestLogin_Errcode(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"errcode":40001,"errmsg":"invalid credential"}`))
	}))
	t.Cleanup(srv.Close)
	c := newTestClient(srv.URL, "https://example.invalid")
	if _, err := c.Login(context.Background()); err == nil || !strings.Contains(err.Error(), "40001") {
		t.Fatalf("Login err = %v, want errcode 40001", err)
	}
}

// TestLogin_NoCreds proves the guard.
func TestLogin_NoCreds(t *testing.T) {
	c := &Client{apiBase: "https://example.invalid", oauth: &oauth2.Config{}}
	if _, err := c.Login(context.Background()); err == nil {
		t.Fatal("want error when credentials not set")
	}
}

// TestWrite_MessageSend proves Write("message",...) routes to POST
// /cgi-bin/message/send with the cached app access_token + JSON body and
// returns the msgid.
func TestWrite_MessageSend(t *testing.T) {
	var gotTok, gotBody string
	mux := http.NewServeMux()
	mux.HandleFunc("/cgi-bin/message/send", func(w http.ResponseWriter, r *http.Request) {
		gotTok = r.URL.Query().Get("access_token")
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"errcode":0,"errmsg":"ok","msgid":"msg-7"}`))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	c := withToken(newTestClient("https://example.invalid/token", srv.URL), suite.Token{AccessToken: "app-tok"})

	msg := []byte(`{"touser":"@all","msgtype":"text","text":{"content":"hi"}}`)
	id, err := c.Write(context.Background(), "message", "", msg)
	if err != nil {
		t.Fatalf("Write: %v", err)
	}
	if id != "msg-7" {
		t.Fatalf("msgid = %q, want msg-7", id)
	}
	if gotTok != "app-tok" {
		t.Fatalf("access_token = %q, want app-tok", gotTok)
	}
	if !strings.Contains(gotBody, "@all") {
		t.Fatalf("send body = %s, want the message JSON", gotBody)
	}
}

// TestWrite_MessageSendError proves a non-zero errcode from send surfaces.
func TestWrite_MessageSendError(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/cgi-bin/message/send", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"errcode":40014,"errmsg":"invalid agentid"}`))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	c := withToken(newTestClient("https://example.invalid/token", srv.URL), suite.Token{AccessToken: "app-tok"})

	if _, err := c.Write(context.Background(), "message", "", []byte("{}")); err == nil || !strings.Contains(err.Error(), "40014") {
		t.Fatalf("Write err = %v, want errcode 40014", err)
	}
}

// TestRead_MessageRead proves Read("message", id) routes to GET
// /cgi-bin/message/get with the cached token + msgid.
func TestRead_MessageRead(t *testing.T) {
	var gotTok, gotMsgID string
	mux := http.NewServeMux()
	mux.HandleFunc("/cgi-bin/message/get", func(w http.ResponseWriter, r *http.Request) {
		gotTok = r.URL.Query().Get("access_token")
		gotMsgID = r.URL.Query().Get("msgid")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"errcode":0,"msg":{"content":"hello"}}`))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	c := withToken(newTestClient("https://example.invalid/token", srv.URL), suite.Token{AccessToken: "app-tok"})

	body, err := c.Read(context.Background(), "message", "msg-7")
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if !strings.Contains(string(body), "hello") {
		t.Fatalf("body = %s, want message content", string(body))
	}
	if gotTok != "app-tok" {
		t.Fatalf("access_token = %q, want app-tok", gotTok)
	}
	if gotMsgID != "msg-7" {
		t.Fatalf("msgid = %q, want msg-7", gotMsgID)
	}
}

// TestRead_MessageReadRequiresID proves an empty id errors before the call.
func TestRead_MessageReadRequiresID(t *testing.T) {
	c := withToken(&Client{}, suite.Token{AccessToken: "at"})
	if _, err := c.Read(context.Background(), "message", ""); err == nil {
		t.Fatal("want error when message read has no id")
	}
}

// TestCachedToken_Guards proves the cached-token guards mirror the other suites.
func TestCachedToken_Guards(t *testing.T) {
	c := &Client{} // no getter
	if _, err := c.cachedToken(context.Background()); err == nil {
		t.Fatal("want error when no token getter wired")
	}
	c2 := withToken(&Client{}, suite.Token{AccessToken: ""})
	if _, err := c2.cachedToken(context.Background()); err == nil {
		t.Fatal("want error when cached token is empty")
	}
}

// TestCachedToken_Expiry proves fix-wework-cached-token-expiry: a token past
// its ExpiresIn is rejected locally instead of being sent to WeCom (errcode
// 42001) — the same defect class fixed for feishu in v0.2.0, left un-fixed in
// the wework client shipped the SAME version.
func TestCachedToken_Expiry(t *testing.T) {
	now := time.Now().Unix()
	cases := []struct {
		name string
		tok  suite.Token
		want string
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

// TestNewClient_Defaults proves the public constructor keeps the contract.
func TestNewClient_Defaults(t *testing.T) {
	c := NewClient("c", "a", "s")
	if c.corpID != "c" || c.agentID != "a" || c.secret != "s" {
		t.Fatal("credentials not set")
	}
	if c.apiBase != defaultAPIBase {
		t.Fatalf("apiBase = %q, want %q", c.apiBase, defaultAPIBase)
	}
	if c.oauth.Endpoint.TokenURL != "https://qyapi.weixin.qq.com/cgi-bin/gettoken" {
		t.Fatalf("token URL = %q", c.oauth.Endpoint.TokenURL)
	}
	if c.Name() != Name || c.Name() != "wework" {
		t.Fatalf("Name = %q, want wework", c.Name())
	}
}
