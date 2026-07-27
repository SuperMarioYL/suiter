// Package dingtalk implements the Suite interface for 钉钉 (DingTalk).
// m1 scope: scaffold only — the OAuth loopback + calendar list/create land in
// m2 behind the same `suiter dingtalk <verb>` grammar and shared TokenStore.
package dingtalk

import (
	"context"
	"fmt"

	"golang.org/x/oauth2"

	"github.com/SuperMarioYL/suiter/internal/suite"
)

const Name = "dingtalk"

// Client is the 钉钉 Suite implementation (m2 stub).
type Client struct {
	appKey      string
	appSecret   string
	oauth       *oauth2.Config // standard OAuth2 — wired in m2
	tokenGetter func(context.Context) (suite.Token, error)
}

// NewClient constructs a 钉钉 client. The OAuth2 config is scaffolded here so
// the loopback plumbing is ready for m2; Login is not wired in m1.
func NewClient(appKey, appSecret string) *Client {
	return &Client{
		appKey:    appKey,
		appSecret: appSecret,
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

// AuthURL builds the OAuth authorize URL (used in m2).
func (c *Client) AuthURL(state string) string { return c.oauth.AuthCodeURL(state) }

// Name returns the suite slug.
func (c *Client) Name() string { return Name }

// Login — m2: runs the OAuth loopback and caches the token via TokenStore.
func (c *Client) Login(ctx context.Context) (suite.Token, error) {
	return suite.Token{}, fmt.Errorf("dingtalk: login lands in m2 (unify three suites)")
}

// Read — m2: calendar list / calendar get.
func (c *Client) Read(ctx context.Context, kind, id string) ([]byte, error) {
	return nil, fmt.Errorf("dingtalk: read lands in m2 (calendar list/get)")
}

// Write — m2: calendar create.
func (c *Client) Write(ctx context.Context, kind, id string, body []byte) (string, error) {
	return "", fmt.Errorf("dingtalk: write lands in m2 (calendar create)")
}
