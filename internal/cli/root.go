// Package cli wires the cobra root command + subcommands for suiter, plus the
// suite registry and the shared TokenStore.
package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	version "github.com/SuperMarioYL/suiter"
	"github.com/SuperMarioYL/suiter/internal/config"
	"github.com/SuperMarioYL/suiter/internal/suite"
	"github.com/SuperMarioYL/suiter/internal/suite/dingtalk"
	"github.com/SuperMarioYL/suiter/internal/suite/feishu"
	"github.com/SuperMarioYL/suiter/internal/suite/tencentdocs"
	"github.com/SuperMarioYL/suiter/internal/suite/wework"
)

const envPrefix = "SUITER"

// NewRoot builds the root command, the suite registry, and the token store.
func NewRoot() *cobra.Command {
	root := &cobra.Command{
		Use:   "suiter",
		Short: "Coding-Agent CLI for 飞书/钉钉/企微/腾讯文档",
		Long: `suiter — googleworkspace/cli 但接了飞书/钉钉/企微/腾讯文档。
4 套 OAuth 收敛成 1 次 ` + "`suiter login`" + `，让你的 Coding Agent 读写你自己的飞书文档、钉钉日历、企微消息、腾讯文档。
所有子命令支持 --json 输出 agent-readable JSON。`,
		SilenceUsage: true,
		// fix-cli-version-self-report-and-surface-drift: cobra only registers
		// a --version flag when root.Version is set. The shipped v0.7.0 binary
		// never set it, so `suiter --version` errored with "unknown flag:
		// --version". Source the version from the canonical VERSION file via
		// go:embed (package version) so no duplicated literal can drift.
		Version: version.Version,
	}

	// viper: env vars + optional config file.
	viper.SetEnvPrefix(envPrefix)
	viper.AutomaticEnv()
	if home, err := os.UserHomeDir(); err == nil {
		viper.SetConfigName("config")
		viper.SetConfigType("yaml")
		viper.AddConfigPath(filepath.Join(home, ".suiter"))
	}

	root.PersistentFlags().Bool("json", false, "output agent-readable JSON")
	root.PersistentFlags().String("config", "", "path to a config file (default ~/.suiter/config.yaml)")

	root.PersistentPreRunE = func(cmd *cobra.Command, args []string) error {
		if cfg, _ := cmd.Flags().GetString("config"); cfg != "" {
			viper.SetConfigFile(cfg)
		}
		_ = viper.ReadInConfig() // best-effort; config is optional
		return nil
	}

	// fix-config-credentials-ignored-before-read: viper does NOT auto-read the
	// config file on GetString, but buildRegistry (below) bakes the credential
	// strings into the suite clients. PersistentPreRunE only runs during
	// Execute() — AFTER NewRoot returns — so a credential set ONLY in the
	// config file was absent when clients were built and every `suiter login`
	// errored with "credentials not set". Read the config here, before
	// buildRegistry, so config-file credentials reach the clients. Env-var
	// override still wins (AutomaticEnv is live above). The --config flag is
	// not parsed by cobra yet at construction time, so resolve it from os.Args
	// so the override applies to credential capture; PersistentPreRunE re-reads
	// (idempotent) to serve runtime viper lookups such as LLM config.
	readConfigBeforeBuild(os.Args)

	store, err := config.NewStore()
	if err != nil {
		// still surface the command tree; commands that need the store error at use.
		fmt.Fprintf(os.Stderr, "warn: token store unavailable: %v\n", err)
	}
	reg := buildRegistry(store)

	root.AddCommand(newLoginCommand(reg, store))
	root.AddCommand(newLogoutCommand(reg, store))
	root.AddCommand(newSuitesCommand(reg))
	root.AddCommand(newFeishuCommand(reg))
	root.AddCommand(newAgentCommand(reg))
	// m2: dingtalk + wework ride the generic suite dispatcher — real
	// implementations behind the Suite interface + registry, zero per-suite
	// CLI glue. m3: tencentdocs joins the same dispatcher now that its
	// Login/Read/Write are real (sheet read/write via net/http + standard OAuth2).
	root.AddCommand(newSuiteCommand(reg, "dingtalk", "钉钉 (DingTalk) — calendar list/create"))
	root.AddCommand(newSuiteCommand(reg, "wework", "企业微信 (WeCom) — message send/read"))
	root.AddCommand(newSuiteCommand(reg, "tencentdocs", "腾讯文档 (Tencent Docs) — sheet read/write"))

	return root
}

// buildRegistry constructs all four suite clients, wires a per-suite token
// getter backed by the shared TokenStore, and registers them.
func buildRegistry(store *config.Store) *suite.Registry {
	reg := suite.NewRegistry()

	tokenGetter := func(name string) func(context.Context) (suite.Token, error) {
		return func(ctx context.Context) (suite.Token, error) {
			if store == nil {
				return suite.Token{}, fmt.Errorf("%s: token store unavailable", name)
			}
			var t suite.Token
			ok, err := store.Load(name, &t)
			if err != nil {
				return suite.Token{}, err
			}
			if !ok {
				return suite.Token{}, fmt.Errorf("%s: not logged in (run `suiter login %s`)", name, name)
			}
			return t, nil
		}
	}

	feishuClient := feishu.NewClient(viper.GetString("feishu_app_id"), viper.GetString("feishu_app_secret"))
	feishuClient.WithTokenGetter(tokenGetter("feishu"))
	reg.Register(feishuClient)

	dingtalkClient := dingtalk.NewClient(viper.GetString("dingtalk_app_key"), viper.GetString("dingtalk_app_secret"))
	dingtalkClient.WithTokenGetter(tokenGetter("dingtalk"))
	reg.Register(dingtalkClient)

	weworkClient := wework.NewClient(viper.GetString("wework_corp_id"), viper.GetString("wework_agent_id"), viper.GetString("wework_secret"))
	weworkClient.WithTokenGetter(tokenGetter("wework"))
	reg.Register(weworkClient)

	tencentClient := tencentdocs.NewClient(viper.GetString("tencentdocs_client_id"), viper.GetString("tencentdocs_client_secret"))
	tencentClient.WithTokenGetter(tokenGetter("tencentdocs"))
	reg.Register(tencentClient)

	return reg
}

// readConfigBeforeBuild resolves the --config override from args (cobra has not
// parsed flags yet at NewRoot time) and best-effort reads the config file so
// config-file credentials are visible when buildRegistry bakes them into the
// suite clients. See fix-config-credentials-ignored-before-read. The read is
// best-effort: a missing config file is not an error (config is optional; env
// vars still supply credentials via AutomaticEnv).
func readConfigBeforeBuild(args []string) {
	if cfg := configPathFromArgs(args); cfg != "" {
		viper.SetConfigFile(cfg)
	}
	_ = viper.ReadInConfig()
}

// configPathFromArgs scans args for --config, returning the path. It handles
// both "--config <path>" and "--config=<path>" forms. Cobra has not parsed
// flags at NewRoot time, so this manual scan is how the --config override is
// applied to credential capture. Returns "" if --config is absent.
func configPathFromArgs(args []string) string {
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--config":
			if i+1 < len(args) {
				return args[i+1]
			}
		case strings.HasPrefix(a, "--config="):
			return strings.TrimPrefix(a, "--config=")
		}
	}
	return ""
}

// newSuitesCommand lists registered suites.
func newSuitesCommand(reg *suite.Registry) *cobra.Command {
	return &cobra.Command{
		Use:   "suites",
		Short: "List registered suites",
		RunE: func(cmd *cobra.Command, args []string) error {
			for _, n := range reg.Names() {
				fmt.Fprintln(cmd.OutOrStdout(), n)
			}
			return nil
		},
	}
}

// newSuiteCommand builds the unified `suiter <suite> <kind> <verb> [id]` tree
// for any registered suite via the shared Suite interface + registry. This is
// the m2 unification primitive: a suite's verbs are read/write on its
// registered Suite, NOT hand-rolled cobra wiring — so wiring dingtalk calendar
// and wework message took zero per-suite CLI glue. Grammar:
//
//	suiter <suite> <kind> read  [id]            → suite.Read(kind, id)
//	suiter <suite> <kind> list  [id]            → suite.Read(kind, id)
//	suiter <suite> <kind> get   [id]            → suite.Read(kind, id)
//	suiter <suite> <kind> create [id]           → suite.Write(kind, id, stdin)
//	suiter <suite> <kind> send   [id]           → suite.Write(kind, id, stdin)
//
// e.g. `suiter dingtalk calendar list --json`, `suiter wework message read <id> --json`.
func newSuiteCommand(reg *suite.Registry, name, short string) *cobra.Command {
	return &cobra.Command{
		Use:   name,
		Short: short,
		Args:  cobra.MinimumNArgs(2), // <kind> <verb> at minimum
		RunE: func(cmd *cobra.Command, args []string) error {
			s, ok := reg.Get(name)
			if !ok {
				return fmt.Errorf("%s suite not registered", name)
			}
			kind := args[0]
			verb := args[1]
			id := ""
			if len(args) >= 3 {
				id = args[2]
			}
			ctx := context.Background()
			out := cmd.OutOrStdout()
			jsonFlag, _ := cmd.Flags().GetBool("json")
			switch verb {
			case "read", "list", "get":
				body, err := s.Read(ctx, kind, id)
				if err != nil {
					return err
				}
				if jsonFlag {
					var pretty bytes.Buffer
					if err := json.Indent(&pretty, body, "", "  "); err == nil {
						fmt.Fprintln(out, pretty.String())
						return nil
					}
				}
				fmt.Fprintln(out, string(body))
				return nil
			case "create", "send", "write":
				body, err := io.ReadAll(cmd.InOrStdin())
				if err != nil {
					return fmt.Errorf("%s: read stdin: %w", name, err)
				}
				idOut, err := s.Write(ctx, kind, id, body)
				if err != nil {
					return err
				}
				if jsonFlag {
					enc := json.NewEncoder(out)
					enc.SetIndent("", "  ")
					_ = enc.Encode(map[string]string{"id": idOut})
					return nil
				}
				fmt.Fprintln(out, idOut)
				return nil
			default:
				return fmt.Errorf("%s: unknown verb %q (read|list|get|create|send)", name, verb)
			}
		},
	}
}
