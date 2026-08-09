package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/SuperMarioYL/suiter/internal/suite"
)

// newFeishuCommand builds the `suiter feishu <verb>` command tree.
// m1: `feishu doc read <id> [--json]`. Other verbs (sheet/bitable/message) land in m2.
func newFeishuCommand(reg *suite.Registry) *cobra.Command {
	feishuCmd := &cobra.Command{
		Use:   "feishu",
		Short: "飞书 (Feishu/Lark)",
	}

	docCmd := &cobra.Command{
		Use:   "doc",
		Short: "飞书文档",
	}

	readCmd := &cobra.Command{
		Use:   "read <doc-id>",
		Short: "Read a Feishu doc body as agent-readable JSON",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			s, ok := reg.Get("feishu")
			if !ok {
				return fmt.Errorf("feishu suite not registered")
			}
			ctx := context.Background()
			body, err := s.Read(ctx, "doc", args[0])
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			jsonFlag, _ := cmd.Flags().GetBool("json")
			if jsonFlag {
				var pretty bytes.Buffer
				if err := json.Indent(&pretty, body, "", "  "); err == nil {
					fmt.Fprintln(out, pretty.String())
					return nil
				}
			}
			fmt.Fprintln(out, string(body))
			return nil
		},
	}

	docCmd.AddCommand(readCmd)
	feishuCmd.AddCommand(docCmd)
	return feishuCmd
}
