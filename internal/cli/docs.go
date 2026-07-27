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

// newAgentCommand builds the `suiter agent run <loop>` tree.
// m3 stub: `agent run summarize-and-schedule <feishu-doc-id>` wires the
// end-to-end loop (read 飞书 → GLM summarize → write 钉钉 calendar event).
func newAgentCommand() *cobra.Command {
	agentCmd := &cobra.Command{
		Use:   "agent",
		Short: "Agent loops (m3)",
	}
	runCmd := &cobra.Command{
		Use:   "run",
		Short: "Run an agent loop",
	}
	sumSchedCmd := &cobra.Command{
		Use:   "summarize-and-schedule <feishu-doc-id>",
		Short: "Read a 飞书 doc → GLM summarize → 钉钉 calendar event (m3)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return fmt.Errorf("agent: summarize-and-schedule lands in m3 (read 飞书 doc → summarize → write 钉钉 calendar event)")
		},
	}
	runCmd.AddCommand(sumSchedCmd)
	agentCmd.AddCommand(runCmd)
	return agentCmd
}
