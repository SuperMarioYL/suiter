// Package tencentdocs implements the Suite interface for 腾讯文档 (Tencent
// Docs). m1 scope: scaffold only — the OAuth loopback + sheet read land in m3.
package tencentdocs

import (
	"context"
	"fmt"

	"golang.org/x/oauth2"

	"github.com/SuperMarioYL/suiter/internal/suite"
)

const Name = "tencentdocs"

// Client is the 腾讯文档 Suite implementation (m3 stub).
type Client struct {
	clientID    string
	clientSecret string
	oauth       *oauth2.Config // standard OAuth2 — wired in m3
	tokenGetter func(context.Context) (suite.Token, error)
}

// NewClient constructs a 腾讯文档 client. The OAuth2 config is scaffolded here
// so the loopback plumbing is ready for m3; Login is not wired in m1.
func NewClient(clientID, clientSecret string) *Client {
	return &Client{
		clientID:     clientID,
		clientSecret: clientSecret,
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

// AuthURL builds the OAuth authorize URL (used in m3).
func (c *Client) AuthURL(state string) string { return c.oauth.AuthCodeURL(state) }

// Name returns the suite slug.
func (c *Client) Name() string { return Name }

// Login — m3: runs the OAuth loopback and caches the token via TokenStore.
func (c *Client) Login(ctx context.Context) (suite.Token, error) {
	return suite.Token{}, fmt.Errorf("tencentdocs: login lands in m3 (agent loop demo)")
}

// Read — m3: sheet read.
func (c *Client) Read(ctx context.Context, kind, id string) ([]byte, error) {
	return nil, fmt.Errorf("tencentdocs: read lands in m3 (sheet read)")
}

// Write — m3: sheet write.
func (c *Client) Write(ctx context.Context, kind, id string, body []byte) (string, error) {
	return "", fmt.Errorf("tencentdocs: write lands in m3 (sheet write)")
}
