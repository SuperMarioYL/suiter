package cli

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/SuperMarioYL/suiter/internal/config"
	"github.com/SuperMarioYL/suiter/internal/suite"
)

// newLoginCommand runs the OAuth loopback for a suite and caches its token.
func newLoginCommand(reg *suite.Registry, store *config.Store) *cobra.Command {
	return &cobra.Command{
		Use:   "login <suite>",
		Short: "Run OAuth loopback for a suite and cache its token",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]
			s, ok := reg.Get(name)
			if !ok {
				return fmt.Errorf("unknown suite %q (run `suiter suites`)", name)
			}
			ctx := context.Background()
			tok, err := s.Login(ctx)
			if err != nil {
				return err
			}
			if store == nil {
				return fmt.Errorf("%s: token store unavailable — cannot cache", name)
			}
			if err := store.Save(name, tok); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "%s: token cached at %s\n", name, store.Path())
			return nil
		},
	}
}
