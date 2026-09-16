package cli

import (
	"encoding/json"
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

// TestSuiteSubcommandsHonorJSONFlag (fix-suite-subcommands-ignore-json-flag)
// proves suites/logout honor --json. The root Long help promises
// "所有子命令支持 --json 输出 agent-readable JSON" and README.md repeats it,
// but v0.8.0 printed plain text on these commands with the flag set — the
// CLI's own advertised agent-readable contract broken on the discovery
// command an agent runs first.
func TestSuiteSubcommandsHonorJSONFlag(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	root := NewRoot()
	var out strings.Builder
	root.SetOut(&out)
	root.SetArgs([]string{"suites", "--json"})
	if err := root.Execute(); err != nil {
		t.Fatalf("suites --json: %v", err)
	}
	var parsed struct {
		Suites []string `json:"suites"`
	}
	if err := json.Unmarshal([]byte(out.String()), &parsed); err != nil {
		t.Fatalf("suites --json output = %q, not JSON (advertised --json contract violated): %v", out.String(), err)
	}
	if len(parsed.Suites) != 4 {
		t.Fatalf("suites = %v, want the 4 registered suites", parsed.Suites)
	}

	// logout --json on a not-logged-in suite emits JSON, not a plain sentence.
	out.Reset()
	root.SetArgs([]string{"logout", "feishu", "--json"})
	if err := root.Execute(); err != nil {
		t.Fatalf("logout feishu --json: %v", err)
	}
	var logoutOut struct {
		Suite  string `json:"suite"`
		Status string `json:"status"`
	}
	if err := json.Unmarshal([]byte(out.String()), &logoutOut); err != nil {
		t.Fatalf("logout feishu --json output = %q, not JSON: %v", out.String(), err)
	}
	if logoutOut.Suite != "feishu" || logoutOut.Status != "not_logged_in" {
		t.Fatalf("logout --json = %+v, want suite=feishu status=not_logged_in", logoutOut)
	}

	// Without --json the plain-text surfaces are byte-identical to v0.8.0.
	// (A fresh root: cobra persistent flag values survive across Executes on
	// the same command, so the earlier --json would leak into these runs.)
	root2 := NewRoot()
	var out2 strings.Builder
	root2.SetOut(&out2)
	root2.SetArgs([]string{"suites"})
	if err := root2.Execute(); err != nil {
		t.Fatalf("suites: %v", err)
	}
	if got, want := out2.String(), "feishu\ndingtalk\nwework\ntencentdocs\n"; got != want {
		t.Fatalf("suites (no --json) = %q, want %q (plain-text contract unchanged)", got, want)
	}
	out2.Reset()
	root2.SetArgs([]string{"logout", "feishu"})
	if err := root2.Execute(); err != nil {
		t.Fatalf("logout feishu: %v", err)
	}
	if got, want := out2.String(), "feishu: not logged in\n"; got != want {
		t.Fatalf("logout feishu (no --json) = %q, want %q (plain-text contract unchanged)", got, want)
	}
}
