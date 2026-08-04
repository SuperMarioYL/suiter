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

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

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

	store, err := config.NewStore()
	if err != nil {
		// still surface the command tree; commands that need the store error at use.
		fmt.Fprintf(os.Stderr, "warn: token store unavailable: %v\n", err)
	}
	reg := buildRegistry(store)

	root.AddCommand(newLoginCommand(reg, store))
	root.AddCommand(newSuitesCommand(reg))
	root.AddCommand(newFeishuCommand(reg))
	root.AddCommand(newAgentCommand())
	// m2: dingtalk + wework ride the generic suite dispatcher — real
	// implementations behind the Suite interface + registry, zero per-suite
	// CLI glue. tencentdocs stays a stub until m3.
	root.AddCommand(newSuiteCommand(reg, "dingtalk", "钉钉 (DingTalk) — calendar list/create"))
	root.AddCommand(newSuiteCommand(reg, "wework", "企业微信 (WeCom) — message send/read"))
	root.AddCommand(newStubSuiteCommand("tencentdocs", "腾讯文档 (Tencent Docs) — m3"))

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

// newStubSuiteCommand is a stub suite command group for m3 suites (tencentdocs).
func newStubSuiteCommand(name, short string) *cobra.Command {
	return &cobra.Command{
		Use:   name,
		Short: short,
		RunE: func(cmd *cobra.Command, args []string) error {
			return fmt.Errorf("%s: subcommands land in m3 (see roadmap)", name)
		},
	}
}
