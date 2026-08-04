// Package dingtalk implements the Suite interface for 钉钉 (DingTalk).
// v0.2.0 (m2): real implementation — OAuth loopback + userAccessToken
// exchange (non-standard JSON contract) + calendar list/get/create via
// net/http, behind the same `suiter <suite> <verb>` grammar and shared
// TokenStore as feishu. No stubs.
package dingtalk

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"runtime"
	"time"

	"github.com/google/uuid"
	"golang.org/x/oauth2"

	"github.com/SuperMarioYL/suiter/internal/suite"
)

const Name = "dingtalk"

// defaultAPIBase is the DingTalk open-api host.
const defaultAPIBase = "https://api.dingtalk.com"

// Client is the 钉钉 Suite implementation (m2).
type Client struct {
	appKey      string
	appSecret   string
	apiBase     string // https://api.dingtalk.com (overridable for tests)
	oauth       *oauth2.Config
	tokenGetter func(context.Context) (suite.Token, error)
}

// NewClient constructs a 钉钉 client. The OAuth2 config carries the authorize
// + token endpoints; the loopback + calendar calls go through net/http.
func NewClient(appKey, appSecret string) *Client {
	return &Client{
		appKey:    appKey,
		appSecret: appSecret,
		apiBase:   defaultAPIBase,
		oauth: &oauth2.Config{
			ClientID:     appKey,
			ClientSecret: appSecret,
			Endpoint: oauth2.Endpoint{
				AuthURL:  "https://login.dingtalk.com/oauth2/auth",
				TokenURL: "https://api.dingtalk.com/v1.0/oauth2/userAccessToken",
			},
		},
	}
}

// WithTokenGetter wires a token loader (backed by the shared TokenStore).
func (c *Client) WithTokenGetter(f func(context.Context) (suite.Token, error)) *Client {
	c.tokenGetter = f
	return c
}

// AuthURL builds the OAuth authorize URL.
func (c *Client) AuthURL(state string) string { return c.oauth.AuthCodeURL(state) }

// Name returns the suite slug.
func (c *Client) Name() string { return Name }

// Login runs the DingTalk user OAuth loopback: open browser → /callback on
// localhost → exchange code → userAccessToken. The token is returned for the
// CLI layer to cache via the shared TokenStore. The loopback mirrors feishu:
// only /callback participates, channel sends are non-blocking (a reload or
// favicon cannot abort or deadlock), and Shutdown has a 5s timeout.
func (c *Client) Login(ctx context.Context) (suite.Token, error) {
	if c.appKey == "" || c.appSecret == "" {
		return suite.Token{}, fmt.Errorf("dingtalk: credentials not set (export SUITER_DINGTALK_APP_KEY and SUITER_DINGTALK_APP_SECRET)")
	}
	state := uuid.NewString()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return suite.Token{}, fmt.Errorf("dingtalk: loopback listen: %w", err)
	}
	defer ln.Close()

	port := ln.Addr().(*net.TCPAddr).Port
	redirect := fmt.Sprintf("http://localhost:%d/callback", port)
	c.oauth.RedirectURL = redirect
	authURL := c.oauth.AuthCodeURL(state)

	codeCh := make(chan string, 1)
	errCh := make(chan error, 1)
	srv := &http.Server{ReadTimeout: 30 * time.Second}
	srv.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/callback" {
			http.NotFound(w, r)
			return
		}
		q := r.URL.Query()
		if e := q.Get("error"); e != "" {
			select {
			case errCh <- fmt.Errorf("dingtalk: oauth error: %s", e):
			default:
			}
			http.Error(w, "oauth error", http.StatusBadRequest)
			return
		}
		if got := q.Get("state"); got != state {
			http.Error(w, "state mismatch", http.StatusBadRequest)
			return
		}
		code := q.Get("code")
		if code == "" {
			http.Error(w, "missing code", http.StatusBadRequest)
			return
		}
		select {
		case codeCh <- code:
		default:
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			fmt.Fprint(w, "<h1>suiter</h1><p>登录已处理，请返回终端。</p>")
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, "<h1>suiter</h1><p>登录成功，请返回终端。</p>")
	})

	go func() { _ = srv.Serve(ln) }()
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	defer srv.Shutdown(shutdownCtx)

	fmt.Fprintf(os.Stderr, "Open this URL in your browser to authorize 钉钉:\n  %s\n", authURL)
	openBrowser(authURL)

	select {
	case code := <-codeCh:
		return c.exchange(ctx, code)
	case err := <-errCh:
		return suite.Token{}, err
	case <-time.After(5 * time.Minute):
		return suite.Token{}, fmt.Errorf("dingtalk: oauth timed out waiting for callback")
	case <-ctx.Done():
		return suite.Token{}, ctx.Err()
	}
}

// exchange swaps an authorize code for a userAccessToken. DingTalk's token
// endpoint is NOT standard oauth2: it expects a JSON body with camelCase
// keys (clientId/clientSecret/grantType/code) and answers with camelCase
// fields (accessToken/expireIn/refreshToken), so the oauth2 package's own
// exchange cannot be used — net/http + JSON it is.
func (c *Client) exchange(ctx context.Context, code string) (suite.Token, error) {
	reqBody := struct {
		ClientID     string `json:"clientId"`
		ClientSecret string `json:"clientSecret"`
		GrantType    string `json:"grantType"`
		Code         string `json:"code"`
	}{ClientID: c.appKey, ClientSecret: c.appSecret, GrantType: "authorization_code", Code: code}
	body, _ := json.Marshal(reqBody)
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, c.oauth.Endpoint.TokenURL, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json; charset=utf-8")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return suite.Token{}, fmt.Errorf("dingtalk: exchange: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return suite.Token{}, fmt.Errorf("dingtalk: exchange status %d: %s", resp.StatusCode, string(raw))
	}
	var tr struct {
		AccessToken  string `json:"accessToken"`
		RefreshToken string `json:"refreshToken"`
		TokenType    string `json:"tokenType"`
		ExpireIn     int64  `json:"expireIn"`
	}
	if err := json.Unmarshal(raw, &tr); err != nil {
		return suite.Token{}, fmt.Errorf("dingtalk: parse exchange: %w", err)
	}
	if tr.AccessToken == "" {
		return suite.Token{}, fmt.Errorf("dingtalk: exchange returned no access token: %s", string(raw))
	}
	return suite.Token{
		AccessToken:  tr.AccessToken,
		RefreshToken: tr.RefreshToken,
		TokenType:    firstNonEmpty(tr.TokenType, "DingTalk"),
		ExpiresIn:    tr.ExpireIn,
		ObtainedAt:   time.Now().Unix(),
	}, nil
}

// Read fetches a 钉钉 resource by kind. m2 implements kind="calendar":
// empty id → list calendars; non-empty id → get one calendar. The raw
// agent-readable JSON body is returned.
func (c *Client) Read(ctx context.Context, kind, id string) ([]byte, error) {
	switch kind {
	case "calendar", "calendars":
		if id == "" {
			return c.calendarList(ctx)
		}
		return c.calendarGet(ctx, id)
	default:
		return nil, fmt.Errorf("dingtalk: read kind %q not implemented", kind)
	}
}

// Write creates a 钉钉 resource by kind. m2 implements kind="calendar":
// create an event in the primary calendar from the JSON body, returning the
// new event id.
func (c *Client) Write(ctx context.Context, kind, id string, body []byte) (string, error) {
	switch kind {
	case "calendar", "calendars":
		return c.calendarCreateEvent(ctx, id, body)
	default:
		return "", fmt.Errorf("dingtalk: write kind %q not implemented", kind)
	}
}

func (c *Client) calendarList(ctx context.Context) ([]byte, error) {
	tok, err := c.cachedToken(ctx)
	if err != nil {
		return nil, err
	}
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, c.apiBase+"/v1.0/calendar/calendars", nil)
	req.Header.Set("x-acs-dingtalk-access-token", tok)
	return c.doRaw(req, "calendar list")
}

func (c *Client) calendarGet(ctx context.Context, id string) ([]byte, error) {
	tok, err := c.cachedToken(ctx)
	if err != nil {
		return nil, err
	}
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, c.apiBase+"/v1.0/calendar/calendars/"+pathEscape(id), nil)
	req.Header.Set("x-acs-dingtalk-access-token", tok)
	return c.doRaw(req, "calendar get")
}

func (c *Client) calendarCreateEvent(ctx context.Context, calendarID string, body []byte) (string, error) {
	tok, err := c.cachedToken(ctx)
	if err != nil {
		return "", err
	}
	if calendarID == "" {
		calendarID = "primary"
	}
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost,
		c.apiBase+"/v1.0/calendar/calendars/"+pathEscape(calendarID)+"/events", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json; charset=utf-8")
	req.Header.Set("x-acs-dingtalk-access-token", tok)
	raw, err := c.doRaw(req, "calendar create")
	if err != nil {
		return "", err
	}
	var r struct {
		ID string `json:"id"`
	}
	_ = json.Unmarshal(raw, &r) // best-effort: surface id if the API returned one
	if r.ID != "" {
		return r.ID, nil
	}
	return "created", nil
}

func (c *Client) cachedToken(ctx context.Context) (string, error) {
	if c.tokenGetter == nil {
		return "", fmt.Errorf("dingtalk: no token getter wired (run `suiter login dingtalk`)")
	}
	tok, err := c.tokenGetter(ctx)
	if err != nil {
		return "", err
	}
	if tok.AccessToken == "" {
		return "", fmt.Errorf("dingtalk: empty cached token (run `suiter login dingtalk`)")
	}
	return tok.AccessToken, nil
}

// doRaw executes the request and returns the body, mapping non-200 to an
// error that carries the kind verb for context.
func (c *Client) doRaw(req *http.Request, verb string) ([]byte, error) {
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("dingtalk: %s: %w", verb, err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("dingtalk: %s status %d: %s", verb, resp.StatusCode, string(raw))
	}
	return raw, nil
}

func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

func pathEscape(s string) string {
	// minimal path-segment escape; DingTalk ids are opaque tokens.
	var b []byte
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c <= ' ' || c == '/' || c == '?' || c == '#' {
			b = append(b, '%')
			b = appendHex(b, c)
			continue
		}
		b = append(b, c)
	}
	return string(b)
}

func appendHex(b []byte, c byte) []byte {
	const hex = "0123456789ABCDEF"
	return append(b, hex[c>>4], hex[c&0x0f])
}

func openBrowser(rawURL string) {
	switch runtime.GOOS {
	case "darwin":
		_ = exec.Command("open", rawURL).Start()
	case "windows":
		_ = exec.Command("rundll32", "url.dll,FileProtocolHandler", rawURL).Start()
	default:
		_ = exec.Command("xdg-open", rawURL).Start()
	}
}
