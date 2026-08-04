// Package feishu implements the Suite interface for 飞书 (Feishu/Lark). The
// larksuite Go SDK v3 client manages app credentials and the in-process token
// cache; the user-level OAuth loopback and docx reads go through net/http for
// v0.1 (typed lark.* API calls land in m2). m1 scope: login + doc read.
package feishu

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/google/uuid"
	lark "github.com/larksuite/oapi-sdk-go/v3"

	"github.com/SuperMarioYL/suiter/internal/suite"
)

const (
	// DefaultBaseURL is the Feishu open platform API base.
	DefaultBaseURL = "https://open.feishu.cn"
	authPath       = "/open-apis/authen/v1/authorize"
	appTokenPath   = "/open-apis/auth/v3/app_access_token/internal"
	oidcTokenPath  = "/open-apis/authen/v1/oidc/access_token"
	// docRawContent — m1 happy path: GET /docx/v1/documents/{id}/raw_content
	docRawContent = "/open-apis/docx/v1/documents/%s/raw_content"
)

// tokenGetter loads the cached token for this suite. Wired by the CLI layer
// against the shared TokenStore so this package stays free of config imports.
type tokenGetter = func(context.Context) (suite.Token, error)

// Client is the 飞书 Suite implementation.
type Client struct {
	appID       string
	appSecret   string
	baseURL     string
	lark        *lark.Client // SDK client managing app credentials + token cache
	tokenGetter tokenGetter
}

// NewClient constructs a Feishu client. The larksuite SDK client manages app
// credentials and the in-process token cache; user OAuth and docx reads go
// through net/http for v0.1 (typed lark.* API calls land in m2).
func NewClient(appID, appSecret string) *Client {
	return &Client{
		appID:     appID,
		appSecret: appSecret,
		baseURL:   DefaultBaseURL,
		lark:      lark.NewClient(appID, appSecret, lark.WithEnableTokenCache(true)),
	}
}

// SDK returns the underlying larksuite SDK client (for m2 typed API calls).
func (c *Client) SDK() *lark.Client { return c.lark }

// WithTokenGetter wires a token loader (backed by the shared TokenStore).
func (c *Client) WithTokenGetter(f func(context.Context) (suite.Token, error)) *Client {
	c.tokenGetter = f
	return c
}

// Name returns the suite slug.
func (c *Client) Name() string { return "feishu" }

// Login runs the Feishu user OAuth loopback: open browser → callback on
// localhost → exchange code → user access token. The token is returned for
// the CLI layer to cache via the shared TokenStore.
func (c *Client) Login(ctx context.Context) (suite.Token, error) {
	if c.appID == "" || c.appSecret == "" {
		return suite.Token{}, fmt.Errorf("feishu: credentials not set (export SUITER_FEISHU_APP_ID and SUITER_FEISHU_APP_SECRET)")
	}
	state := uuid.NewString()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return suite.Token{}, fmt.Errorf("feishu: loopback listen: %w", err)
	}
	defer ln.Close()

	port := ln.Addr().(*net.TCPAddr).Port
	redirect := fmt.Sprintf("http://localhost:%d/callback", port)
	authURL := c.authURL(redirect, state)

	codeCh := make(chan string, 1)
	errCh := make(chan error, 1)
	srv := &http.Server{ReadTimeout: 30 * time.Second}
	srv.Handler = c.oauthHandler(state, codeCh, errCh)

	go func() { _ = srv.Serve(ln) }()
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	defer srv.Shutdown(shutdownCtx)

	fmt.Fprintf(os.Stderr, "Open this URL in your browser to authorize 飞书:\n  %s\n", authURL)
	openBrowser(authURL)

	select {
	case code := <-codeCh:
		return c.exchange(ctx, code)
	case err := <-errCh:
		return suite.Token{}, err
	case <-time.After(5 * time.Minute):
		return suite.Token{}, fmt.Errorf("feishu: oauth timed out waiting for callback")
	case <-ctx.Done():
		return suite.Token{}, ctx.Err()
	}
}

// oauthHandler builds the loopback callback handler. Only the /callback path
// participates in the OAuth flow; every other request (favicon, probes, a
// second browser connection) is 404'd WITHOUT touching the channels so it can
// neither abort login via errCh nor block Shutdown by hanging on a full
// channel send. All sends are non-blocking (select/default) so only the first
// callback wins and a reload/second connection cannot deadlock the handler.
func (c *Client) oauthHandler(state string, codeCh chan string, errCh chan error) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/callback" {
			http.NotFound(w, r)
			return
		}
		q := r.URL.Query()
		if e := q.Get("error"); e != "" {
			select {
			case errCh <- fmt.Errorf("feishu: oauth error: %s", e):
			default:
			}
			http.Error(w, "oauth error", http.StatusBadRequest)
			return
		}
		// A stray request with the wrong state (a replay, second connection,
		// or probe that happened to hit /callback) must NOT abort the flow:
		// respond 400 without pushing to errCh so only the real callback wins.
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
}

func (c *Client) authURL(redirect, state string) string {
	q := url.Values{}
	q.Set("app_id", c.appID)
	q.Set("redirect_uri", redirect)
	q.Set("state", state)
	return c.baseURL + authPath + "?" + q.Encode()
}

func (c *Client) exchange(ctx context.Context, code string) (suite.Token, error) {
	appTok, err := c.appAccessToken(ctx)
	if err != nil {
		return suite.Token{}, err
	}
	// The official larksuite oapi-sdk-go sends a JSON body (not form-encoded),
	// so match that contract: {"grant_type":"authorization_code","code":...}.
	reqBody := struct {
		GrantType string `json:"grant_type"`
		Code      string `json:"code"`
	}{GrantType: "authorization_code", Code: code}
	body, _ := json.Marshal(reqBody)
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+oidcTokenPath, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json; charset=utf-8")
	req.Header.Set("Authorization", "Bearer "+appTok)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return suite.Token{}, fmt.Errorf("feishu: exchange: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return suite.Token{}, fmt.Errorf("feishu: exchange status %d: %s", resp.StatusCode, string(raw))
	}
	// Feishu nests the token fields under a top-level "data" object (per the
	// larksuite SDK's CreateOidcAccessTokenResp.Data): code/msg stay at the
	// top level so the guard still works, but access_token et al. live in
	// tr.Data — reading them at the top level left AccessToken "" and cached
	// an empty token (the m1 star-moment break). Build the Token from tr.Data.
	var tr struct {
		Code int    `json:"code"`
		Msg  string `json:"msg"`
		Data struct {
			AccessToken      string `json:"access_token"`
			RefreshToken     string `json:"refresh_token"`
			TokenType        string `json:"token_type"`
			ExpiresIn        int64  `json:"expires_in"`
			RefreshExpiresIn int64  `json:"refresh_expires_in"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &tr); err != nil {
		return suite.Token{}, fmt.Errorf("feishu: parse exchange: %w", err)
	}
	if tr.Code != 0 {
		return suite.Token{}, fmt.Errorf("feishu: exchange code=%d msg=%s", tr.Code, tr.Msg)
	}
	return suite.Token{
		AccessToken:  tr.Data.AccessToken,
		RefreshToken: tr.Data.RefreshToken,
		TokenType:    firstNonEmpty(tr.Data.TokenType, "Bearer"),
		ExpiresIn:    tr.Data.ExpiresIn,
		ObtainedAt:   time.Now().Unix(),
	}, nil
}

func (c *Client) appAccessToken(ctx context.Context) (string, error) {
	body := fmt.Sprintf(`{"app_id":%q,"app_secret":%q}`, c.appID, c.appSecret)
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+appTokenPath, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json; charset=utf-8")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("feishu: app_access_token: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("feishu: app_access_token status %d: %s", resp.StatusCode, string(raw))
	}
	var tr struct {
		Code           int    `json:"code"`
		Msg            string `json:"msg"`
		AppAccessToken string `json:"app_access_token"`
	}
	if err := json.Unmarshal(raw, &tr); err != nil {
		return "", fmt.Errorf("feishu: parse app_access_token: %w", err)
	}
	if tr.Code != 0 {
		return "", fmt.Errorf("feishu: app_access_token code=%d msg=%s", tr.Code, tr.Msg)
	}
	return tr.AppAccessToken, nil
}

// Read fetches a Feishu resource by kind. m1 implements kind="doc" → docx raw
// content. Other kinds (sheet/bitable/message) land in m2.
func (c *Client) Read(ctx context.Context, kind, id string) ([]byte, error) {
	tok, err := c.cachedToken(ctx)
	if err != nil {
		return nil, err
	}
	switch kind {
	case "doc", "document":
		return c.readDocRawContent(ctx, tok, id)
	default:
		return nil, fmt.Errorf("feishu: read kind %q not implemented (m2)", kind)
	}
}

// Write creates or updates a Feishu resource. Not implemented in m1 (m2).
func (c *Client) Write(ctx context.Context, kind, id string, body []byte) (string, error) {
	return "", fmt.Errorf("feishu: write not implemented in m1 (m2)")
}

func (c *Client) readDocRawContent(ctx context.Context, accessToken, docID string) ([]byte, error) {
	path := fmt.Sprintf(docRawContent, url.PathEscape(docID))
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+path, nil)
	req.Header.Set("Authorization", "Bearer "+accessToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("feishu: doc read: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("feishu: doc read status %d: %s", resp.StatusCode, string(raw))
	}
	return raw, nil
}

func (c *Client) cachedToken(ctx context.Context) (string, error) {
	if c.tokenGetter == nil {
		return "", fmt.Errorf("feishu: no token getter wired (run `suiter login feishu`)")
	}
	tok, err := c.tokenGetter(ctx)
	if err != nil {
		return "", err
	}
	if tok.AccessToken == "" {
		return "", fmt.Errorf("feishu: empty cached token (run `suiter login feishu`)")
	}
	// A token cached hours/days ago (Feishu user access tokens expire ~2h)
	// used to be returned unconditionally and Feishu answered 401 with an
	// opaque error. Treat staleness locally: if the cached token is past its
	// ExpiresIn, ask the user to re-login instead of sending a dead token.
	// Full refresh-token logic is a later feature; this just stops silent use.
	if tok.ExpiresIn > 0 && time.Now().Unix()-tok.ObtainedAt >= tok.ExpiresIn {
		return "", fmt.Errorf("feishu: token expired (re-run `suiter login feishu`)")
	}
	return tok.AccessToken, nil
}

func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
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
