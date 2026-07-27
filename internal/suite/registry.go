// Package suite defines the unified Suite interface that all four CN office
// suites (飞书 / 钉钉 / 企微 / 腾讯文档) implement, plus the Registry that
// resolves a suite by name. One interface, one grammar: suiter <suite> <verb>.
package suite

import "context"

// Token is the per-suite cached credential returned by Login.
type Token struct {
	AccessToken  string         `json:"access_token"`
	RefreshToken string         `json:"refresh_token,omitempty"`
	TokenType    string         `json:"token_type,omitempty"`
	ExpiresIn    int64          `json:"expires_in,omitempty"` // seconds
	ObtainedAt   int64          `json:"obtained_at"`          // unix seconds
	Raw          map[string]any `json:"raw,omitempty"`
}

// Suite is the single primitive a registry serves for all four suites.
// One interface, one grammar: suiter <suite> <verb> <id>.
type Suite interface {
	// Name is the suite slug: feishu | dingtalk | wework | tencentdocs.
	Name() string
	// Login runs the suite-native OAuth loopback and returns a Token for the
	// CLI layer to cache in the shared TokenStore.
	Login(ctx context.Context) (Token, error)
	// Read fetches a resource (doc/sheet/calendar/message) by kind+id and
	// returns an agent-readable JSON body.
	Read(ctx context.Context, kind, id string) ([]byte, error)
	// Write creates or updates a resource and returns its id.
	Write(ctx context.Context, kind, id string, body []byte) (string, error)
}

// Registry maps suite name → Suite.
type Registry struct {
	suites map[string]Suite
	order  []string
}

// NewRegistry returns an empty Registry.
func NewRegistry() *Registry {
	return &Registry{suites: make(map[string]Suite)}
}

// Register adds a Suite to the registry.
func (r *Registry) Register(s Suite) {
	if _, ok := r.suites[s.Name()]; !ok {
		r.order = append(r.order, s.Name())
	}
	r.suites[s.Name()] = s
}

// Get looks up a Suite by name.
func (r *Registry) Get(name string) (Suite, bool) {
	s, ok := r.suites[name]
	return s, ok
}

// Names returns registered suite names in registration order.
func (r *Registry) Names() []string {
	return append([]string(nil), r.order...)
}
