// Package wework implements the Suite interface for 企业微信 (WeCom).
// v0.2.0 (m2): real implementation — WeCom app access_token via gettoken
// (corpid+secret, server-to-server; no browser loopback — WeCom app tokens
// do not require user consent) + message send/read via net/http, behind the
// same `suiter <suite> <verb>` grammar and shared TokenStore as feishu and
// dingtalk. No stubs.
package wework

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"golang.org/x/oauth2"

	"github.com/SuperMarioYL/suiter/internal/suite"
)

const Name = "wework"

// defaultAPIBase is the WeCom open-api host.
const defaultAPIBase = "https://qyapi.weixin.qq.com"

// Client is the 企业微信 Suite implementation (m2).
type Client struct {
	corpID      string
	agentID     string
	secret      string
	apiBase     string // https://qyapi.weixin.qq.com (overridable for tests)
	oauth       *oauth2.Config
	tokenGetter func(context.Context) (suite.Token, error)
}

// NewClient constructs a WeCom client. The OAuth2 config carries the
// gettoken endpoint; message send/read go through net/http.
func NewClient(corpID, agentID, secret string) *Client {
	return &Client{
		corpID:  corpID,
		agentID: agentID,
		secret:  secret,
		apiBase: defaultAPIBase,
		oauth: &oauth2.Config{
			ClientID:     corpID,
			ClientSecret: secret,
			Endpoint: oauth2.Endpoint{
				AuthURL:  "https://open.weixin.qq.com/connect/qrconnect",
				TokenURL: "https://qyapi.weixin.qq.com/cgi-bin/gettoken",
			},
		},
	}
}

// WithTokenGetter wires a token loader (backed by the shared TokenStore).
func (c *Client) WithTokenGetter(f func(context.Context) (suite.Token, error)) *Client {
	c.tokenGetter = f
	return c
}

// AuthURL builds the OAuth authorize URL (qrconnect; user-identity flows land later).
func (c *Client) AuthURL(state string) string { return c.oauth.AuthCodeURL(state) }

// Name returns the suite slug.
func (c *Client) Name() string { return Name }

// Login fetches the WeCom app access_token via gettoken (corpid+secret). WeCom
// app tokens are server-to-server — no browser loopback or user consent is
// required — so this "login" is a single authenticated POST that caches the
// token via the shared TokenStore, the same `suiter login <suite>` grammar as
// feishu/dingtalk behind the Suite interface.
func (c *Client) Login(ctx context.Context) (suite.Token, error) {
	if c.corpID == "" || c.secret == "" {
		return suite.Token{}, fmt.Errorf("wework: credentials not set (export SUITER_WEWORK_CORP_ID and SUITER_WEWORK_SECRET)")
	}
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet,
		c.oauth.Endpoint.TokenURL+"?corpid="+c.corpID+"&corpsecret="+c.secret, nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return suite.Token{}, fmt.Errorf("wework: gettoken: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return suite.Token{}, fmt.Errorf("wework: gettoken status %d: %s", resp.StatusCode, string(raw))
	}
	var tr struct {
		ErrCode     int    `json:"errcode"`
		ErrMsg      string `json:"errmsg"`
		AccessToken string `json:"access_token"`
		ExpiresIn   int64  `json:"expires_in"`
	}
	if err := json.Unmarshal(raw, &tr); err != nil {
		return suite.Token{}, fmt.Errorf("wework: parse gettoken: %w", err)
	}
	if tr.ErrCode != 0 {
		return suite.Token{}, fmt.Errorf("wework: gettoken errcode=%d msg=%s", tr.ErrCode, tr.ErrMsg)
	}
	if tr.AccessToken == "" {
		return suite.Token{}, fmt.Errorf("wework: gettoken returned no access_token: %s", string(raw))
	}
	return suite.Token{
		AccessToken: tr.AccessToken,
		TokenType:   "WeCom",
		ExpiresIn:   tr.ExpiresIn,
		ObtainedAt:  time.Now().Unix(),
	}, nil
}

// Read fetches a WeCom resource by kind. m2 implements kind="message": read
// a message by id via the WeCom message API surface (net/http, app
// access_token). The raw agent-readable JSON body is returned.
func (c *Client) Read(ctx context.Context, kind, id string) ([]byte, error) {
	switch kind {
	case "message", "messages":
		return c.messageRead(ctx, id)
	default:
		return nil, fmt.Errorf("wework: read kind %q not implemented", kind)
	}
}

// Write creates a WeCom resource by kind. m2 implements kind="message": send
// an app message from the JSON body, returning the msgid WeCom assigned.
func (c *Client) Write(ctx context.Context, kind, id string, body []byte) (string, error) {
	switch kind {
	case "message", "messages":
		return c.messageSend(ctx, body)
	default:
		return "", fmt.Errorf("wework: write kind %q not implemented", kind)
	}
}

func (c *Client) messageSend(ctx context.Context, body []byte) (string, error) {
	tok, err := c.cachedToken(ctx)
	if err != nil {
		return "", err
	}
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost,
		c.apiBase+"/cgi-bin/message/send?access_token="+tok, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json; charset=utf-8")
	raw, err := c.doRaw(req, "message send")
	if err != nil {
		return "", err
	}
	var r struct {
		ErrCode int    `json:"errcode"`
		ErrMsg  string `json:"errmsg"`
		MsgID   string `json:"msgid"`
	}
	_ = json.Unmarshal(raw, &r)
	if r.ErrCode != 0 {
		return "", fmt.Errorf("wework: message send errcode=%d msg=%s", r.ErrCode, r.ErrMsg)
	}
	if r.MsgID != "" {
		return r.MsgID, nil
	}
	return "sent", nil
}

func (c *Client) messageRead(ctx context.Context, id string) ([]byte, error) {
	tok, err := c.cachedToken(ctx)
	if err != nil {
		return nil, err
	}
	if id == "" {
		return nil, fmt.Errorf("wework: message read requires a message id")
	}
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet,
		c.apiBase+"/cgi-bin/message/get?access_token="+tok+"&msgid="+id, nil)
	return c.doRaw(req, "message read")
}

func (c *Client) cachedToken(ctx context.Context) (string, error) {
	if c.tokenGetter == nil {
		return "", fmt.Errorf("wework: no token getter wired (run `suiter login wework`)")
	}
	tok, err := c.tokenGetter(ctx)
	if err != nil {
		return "", err
	}
	if tok.AccessToken == "" {
		return "", fmt.Errorf("wework: empty cached token (run `suiter login wework`)")
	}
	// A token cached >2h ago (WeCom app access_token expires ~7200s) used to be
	// returned unconditionally and WeCom answered errcode 42001 with an opaque
	// "wework: message send errcode=42001" error. Treat staleness locally —
	// mirror the feishu fix: if the cached token is past its ExpiresIn, ask the
	// user to re-login instead of sending a dead token. Full refresh-token logic
	// is a later feature; this just stops silent use of stale tokens.
	if tok.ExpiresIn > 0 && time.Now().Unix()-tok.ObtainedAt >= tok.ExpiresIn {
		return "", fmt.Errorf("wework: token expired (re-run `suiter login wework`)")
	}
	return tok.AccessToken, nil
}

// doRaw executes the request and returns the body, mapping non-200 to an
// error that carries the verb for context.
func (c *Client) doRaw(req *http.Request, verb string) ([]byte, error) {
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("wework: %s: %w", verb, err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("wework: %s status %d: %s", verb, resp.StatusCode, string(raw))
	}
	return raw, nil
}
