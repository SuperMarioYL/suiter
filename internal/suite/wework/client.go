// Package wework implements the Suite interface for 企业微信 (WeCom).
// m1 scope: scaffold only — the OAuth loopback + message send/read land in m2.
package wework

import (
	"context"
	"fmt"

	"golang.org/x/oauth2"

	"github.com/SuperMarioYL/suiter/internal/suite"
)

const Name = "wework"

// Client is the 企业微信 Suite implementation (m2 stub).
type Client struct {
	corpID      string
	agentID     string
	secret      string
	oauth       *oauth2.Config // standard OAuth2 — wired in m2
	tokenGetter func(context.Context) (suite.Token, error)
}

// NewClient constructs a WeCom client. The OAuth2 config is scaffolded here so
// the loopback plumbing is ready for m2; Login is not wired in m1.
func NewClient(corpID, agentID, secret string) *Client {
	return &Client{
		corpID:   corpID,
		agentID:  agentID,
		secret:   secret,
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

// AuthURL builds the OAuth authorize URL (used in m2).
func (c *Client) AuthURL(state string) string { return c.oauth.AuthCodeURL(state) }

// Name returns the suite slug.
func (c *Client) Name() string { return Name }

// Login — m2: runs the OAuth loopback and caches the token via TokenStore.
func (c *Client) Login(ctx context.Context) (suite.Token, error) {
	return suite.Token{}, fmt.Errorf("wework: login lands in m2 (unify three suites)")
}

// Read — m2: message read.
func (c *Client) Read(ctx context.Context, kind, id string) ([]byte, error) {
	return nil, fmt.Errorf("wework: read lands in m2 (message read)")
}

// Write — m2: message send.
func (c *Client) Write(ctx context.Context, kind, id string, body []byte) (string, error) {
	return "", fmt.Errorf("wework: write lands in m2 (message send)")
}
