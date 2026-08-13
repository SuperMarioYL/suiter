package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/viper"

	"github.com/SuperMarioYL/suiter/internal/suite/dingtalk"
)

// setupViperForTest replicates NewRoot's viper env setup (env prefix +
// automatic env) so buildRegistry sees env vars the same way production does.
// viper.Reset clears any state left by NewRoot's ReadInConfig.
func setupViperForTest(t *testing.T) {
	t.Helper()
	viper.Reset()
	viper.SetEnvPrefix(envPrefix)
	viper.AutomaticEnv()
}

// TestBuildRegistry_ConfigFileCredentials (fix-config-credentials-ignored-before-read)
// proves a suite credential set ONLY in a config file — not an env var — is
// visible to the client-construction path: buildRegistry bakes the config-file
// value into the dingtalk client, observable via AuthURL (its client_id query
// param). This is exactly the regression the fake-wired agent/dispatcher tests
// missed (they wire fakes directly, never exercising viper), so a credential
// present only in ~/.suiter/config.yaml was silently ignored and every
// `suiter login <suite>` errored with "credentials not set". It also proves the
// env-var override still wins over the config file (AutomaticEnv stays live).
func TestBuildRegistry_ConfigFileCredentials(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")
	// YAML keys map 1:1 to the viper.GetString keys buildRegistry uses.
	cfg := []byte(strings.Join([]string{
		"feishu_app_id: f-cfg-id",
		"feishu_app_secret: f-cfg-secret",
		"dingtalk_app_key: d-cfg-key",
		"dingtalk_app_secret: d-cfg-secret",
		"",
	}, "\n"))
	if err := os.WriteFile(cfgPath, cfg, 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	setupViperForTest(t)
	viper.SetConfigFile(cfgPath)
	if err := viper.ReadInConfig(); err != nil {
		t.Fatalf("ReadInConfig: %v", err)
	}
	t.Cleanup(viper.Reset)

	// buildRegistry(nil) is the exact client-construction path NewRoot calls;
	// a nil store is fine — only the per-suite token getter checks store == nil,
	// and it is not invoked at construction time.
	reg := buildRegistry(nil)
	dc, ok := reg.Get("dingtalk")
	if !ok || dc == nil {
		t.Fatal("dingtalk suite not registered")
	}
	dcClient, ok := dc.(*dingtalk.Client)
	if !ok {
		t.Fatalf("dingtalk suite = %T, want *dingtalk.Client", dc)
	}
	// AuthURL surfaces the configured appKey as client_id in the authorize URL.
	got := dcClient.AuthURL("st")
	if !strings.Contains(got, "client_id=d-cfg-key") {
		t.Fatalf("AuthURL = %q, want client_id=d-cfg-key (config-file credential ignored at client build)", got)
	}

	// Env-var override must still win over the config file: AutomaticEnv is live,
	// so a credential exported as SUITER_DINGTALK_APP_KEY supersedes the file.
	t.Setenv("SUITER_DINGTALK_APP_KEY", "d-env-key")
	reg2 := buildRegistry(nil)
	dc2, _ := reg2.Get("dingtalk")
	got2 := dc2.(*dingtalk.Client).AuthURL("st")
	if !strings.Contains(got2, "client_id=d-env-key") {
		t.Fatalf("AuthURL = %q, want client_id=d-env-key (env-var override over config file broken)", got2)
	}
}

// TestConfigPathFromArgs proves the --config override is resolved from os.Args
// at NewRoot time (before cobra parses flags), in both " --config <path> " and
// " --config=<path> " forms, and returns "" when absent. This is what lets a
// `--config`-supplied file apply to credential capture at construction time.
func TestConfigPathFromArgs(t *testing.T) {
	cases := []struct {
		name string
		args []string
		want string
	}{
		{"absent", []string{"suiter", "suites"}, ""},
		{"space-form", []string{"suiter", "--config", "/tmp/c.yaml", "suites"}, "/tmp/c.yaml"},
		{"equals-form", []string{"suiter", "--config=/tmp/c.yaml", "suites"}, "/tmp/c.yaml"},
		{"space-form-trailing-flag", []string{"suiter", "--config"}, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := configPathFromArgs(tc.args); got != tc.want {
				t.Fatalf("configPathFromArgs(%v) = %q, want %q", tc.args, got, tc.want)
			}
		})
	}
}
