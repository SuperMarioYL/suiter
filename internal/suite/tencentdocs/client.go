// Package tencentdocs implements the Suite interface for 腾讯文档 (Tencent
// Docs). v0.3.0 (m3): real implementation — standard OAuth2 loopback + sheet
// read/write via net/http, behind the same `suiter <suite> <verb>` grammar and
// shared TokenStore as the other suites. The agent loop (read 飞书 → GLM
// summarize → write 钉钉 calendar) consumes this via `suiter agent run
// summarize-and-schedule`. No stubs.
package tencentdocs

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

const Name = "tencentdocs"

// defaultAPIBase is the 腾讯文档 open-api host.
const defaultAPIBase = "https://docs.qq.com"

// Client is the 腾讯文档 Suite implementation (m3).
type Client struct {
	clientID     string
	clientSecret string
	apiBase      string // https://docs.qq.com (overridable for tests)
	oauth        *oauth2.Config
	tokenGetter  func(context.Context) (suite.Token, error)
}

// NewClient constructs a 腾讯文档 client. The OAuth2 config carries the
// authorize + token endpoints; the loopback + sheet calls go through net/http.
func NewClient(clientID, clientSecret string) *Client {
	return &Client{
		clientID:     clientID,
		clientSecret: clientSecret,
		apiBase:      defaultAPIBase,
		oauth: &oauth2.Config{
			ClientID:     clientID,
			ClientSecret: clientSecret,
			Endpoint: oauth2.Endpoint{
				AuthURL:  "https://open.tencent.com/oauth2/authorize",
				TokenURL: "https://open.tencent.com/oauth2/token",
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

// Login runs the 腾讯文档 user OAuth loopback: open browser → /callback on
// localhost → exchange code → access token. The token is returned for the CLI
// layer to cache via the shared TokenStore. The loopback mirrors dingtalk:
// only /callback participates, channel sends are non-blocking (a reload or
// favicon cannot abort or deadlock), and Shutdown has a 5s timeout. 腾讯文档
// uses standard OAuth2 (unlike DingTalk's non-standard camelCase contract), so
// the exchange is a standard JSON body + snake_case response.
func (c *Client) Login(ctx context.Context) (suite.Token, error) {
	if c.clientID == "" || c.clientSecret == "" {
		return suite.Token{}, fmt.Errorf("tencentdocs: credentials not set (export SUITER_TENCENTDOCS_CLIENT_ID and SUITER_TENCENTDOCS_CLIENT_SECRET)")
	}
	state := uuid.NewString()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return suite.Token{}, fmt.Errorf("tencentdocs: loopback listen: %w", err)
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
			case errCh <- fmt.Errorf("tencentdocs: oauth error: %s", e):
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

	fmt.Fprintf(os.Stderr, "Open this URL in your browser to authorize 腾讯文档:\n  %s\n", authURL)
	openBrowser(authURL)

	select {
	case code := <-codeCh:
		return c.exchange(ctx, code)
	case err := <-errCh:
		return suite.Token{}, err
	case <-time.After(5 * time.Minute):
		return suite.Token{}, fmt.Errorf("tencentdocs: oauth timed out waiting for callback")
	case <-ctx.Done():
		return suite.Token{}, ctx.Err()
	}
}

// exchange swaps an authorize code for an access token. 腾讯文档 uses standard
// OAuth2: a JSON body {grant_type, code, client_id, client_secret} and a
// standard snake_case response (access_token/refresh_token/token_type/expires_in).
func (c *Client) exchange(ctx context.Context, code string) (suite.Token, error) {
	reqBody := struct {
		GrantType    string `json:"grant_type"`
		Code         string `json:"code"`
		ClientID     string `json:"client_id"`
		ClientSecret string `json:"client_secret"`
	}{GrantType: "authorization_code", Code: code, ClientID: c.clientID, ClientSecret: c.clientSecret}
	body, _ := json.Marshal(reqBody)
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, c.oauth.Endpoint.TokenURL, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json; charset=utf-8")
	resp, err := suite.HTTPClient().Do(req)
	if err != nil {
		return suite.Token{}, fmt.Errorf("tencentdocs: exchange: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return suite.Token{}, fmt.Errorf("tencentdocs: exchange status %d: %s", resp.StatusCode, string(raw))
	}
	var tr struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		TokenType    string `json:"token_type"`
		ExpiresIn    int64  `json:"expires_in"`
	}
	if err := json.Unmarshal(raw, &tr); err != nil {
		return suite.Token{}, fmt.Errorf("tencentdocs: parse exchange: %w", err)
	}
	if tr.AccessToken == "" {
		return suite.Token{}, fmt.Errorf("tencentdocs: exchange returned no access token: %s", string(raw))
	}
	return suite.Token{
		AccessToken:  tr.AccessToken,
		RefreshToken: tr.RefreshToken,
		TokenType:    firstNonEmpty(tr.TokenType, "Bearer"),
		ExpiresIn:    tr.ExpiresIn,
		ObtainedAt:   time.Now().Unix(),
	}, nil
}

// Read fetches a 腾讯文档 resource by kind. m3 implements kind="sheet": read a
// spreadsheet's raw content (cells/ranges) as agent-readable JSON.
func (c *Client) Read(ctx context.Context, kind, id string) ([]byte, error) {
	switch kind {
	case "sheet", "spreadsheet":
		return c.sheetRead(ctx, id)
	default:
		return nil, fmt.Errorf("tencentdocs: read kind %q not implemented", kind)
	}
}

// Write creates or updates a 腾讯文档 resource by kind. m3 implements
// kind="sheet": write cells/ranges, returning the affected range id.
func (c *Client) Write(ctx context.Context, kind, id string, body []byte) (string, error) {
	switch kind {
	case "sheet", "spreadsheet":
		return c.sheetWrite(ctx, id, body)
	default:
		return "", fmt.Errorf("tencentdocs: write kind %q not implemented", kind)
	}
}

func (c *Client) sheetRead(ctx context.Context, sheetID string) ([]byte, error) {
	tok, err := c.cachedToken(ctx)
	if err != nil {
		return nil, err
	}
	if sheetID == "" {
		return nil, fmt.Errorf("tencentdocs: sheet read requires a sheet id")
	}
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet,
		c.apiBase+"/api/v1/spreadsheets/"+pathEscape(sheetID), nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	return c.doRaw(req, "sheet read")
}

func (c *Client) sheetWrite(ctx context.Context, sheetID string, body []byte) (string, error) {
	tok, err := c.cachedToken(ctx)
	if err != nil {
		return "", err
	}
	if sheetID == "" {
		return "", fmt.Errorf("tencentdocs: sheet write requires a sheet id")
	}
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost,
		c.apiBase+"/api/v1/spreadsheets/"+pathEscape(sheetID)+"/values", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json; charset=utf-8")
	req.Header.Set("Authorization", "Bearer "+tok)
	raw, err := c.doRaw(req, "sheet write")
	if err != nil {
		return "", err
	}
	// fix-dingtalk-tencentdocs-write-fake-success: the v0.5.0 read-path fix
	// guarded feishu/wework reads against HTTP-200 error envelopes, but the
	// WRITE path was not touched — sheetWrite unmarshaled `range`/`id` and
	// returned the hard-coded literal "written" when neither was present,
	// masking HTTP-200 error envelopes (a real Tencent-Docs failure mode for
	// permission/validation errors) and wrong-field successes. Mirror the
	// read-path guard: surface the API error envelope (Tencent `code`/
	// `errcode`/`ret` + `errmsg`/`message`) as an error; only return an id/
	// range when the body actually carries one; otherwise surface the raw
	// body so a wrong-field success is observable rather than silently
	// returned as "written" (which made the agent loop exit 0, losing data).
	var r struct {
		Range   string `json:"range"`
		ID      string `json:"id"`
		Code    int    `json:"code"`    // some Tencent envelopes use int code
		ErrCode int    `json:"errcode"` // legacy envelope
		Ret     int    `json:"ret"`     // QQ-family ret/errmsg
		ErrMsg  string `json:"errmsg"`
		Message string `json:"message"`
	}
	_ = json.Unmarshal(raw, &r)
	if r.Code != 0 || r.ErrCode != 0 || r.Ret != 0 {
		return "", fmt.Errorf("tencentdocs: sheet write code=%d errcode=%d ret=%d msg=%s",
			r.Code, r.ErrCode, r.Ret, firstNonEmpty(r.ErrMsg, r.Message))
	}
	if r.ID != "" {
		return r.ID, nil
	}
	if r.Range != "" {
		return r.Range, nil
	}
	return "", fmt.Errorf("tencentdocs: sheet write: no id in response: %s", string(raw))
}

func (c *Client) cachedToken(ctx context.Context) (string, error) {
	if c.tokenGetter == nil {
		return "", fmt.Errorf("tencentdocs: no token getter wired (run `suiter login tencentdocs`)")
	}
	tok, err := c.tokenGetter(ctx)
	if err != nil {
		return "", err
	}
	if tok.AccessToken == "" {
		return "", fmt.Errorf("tencentdocs: empty cached token (run `suiter login tencentdocs`)")
	}
	// Mirror the feishu/dingtalk/wework staleness guard: never send a token
	// past its ExpiresIn (腾讯文档 access tokens expire ~2h). Full
	// refresh-token logic is a later feature; this stops silent stale-token use.
	if tok.ExpiresIn > 0 && time.Now().Unix()-tok.ObtainedAt >= tok.ExpiresIn {
		return "", fmt.Errorf("tencentdocs: token expired (re-run `suiter login tencentdocs`)")
	}
	return tok.AccessToken, nil
}

// doRaw executes the request and returns the body, mapping non-200 to an
// error that carries the kind verb for context.
func (c *Client) doRaw(req *http.Request, verb string) ([]byte, error) {
	resp, err := suite.HTTPClient().Do(req)
	if err != nil {
		return nil, fmt.Errorf("tencentdocs: %s: %w", verb, err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("tencentdocs: %s status %d: %s", verb, resp.StatusCode, string(raw))
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
