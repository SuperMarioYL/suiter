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

// newLogoutCommand clears a suite's cached token from the shared TokenStore
// (feat-logout-command). Wires the previously-dead Store.Delete — which had ZERO
// non-test callers — completing the login→use→logout lifecycle the
// v0.2.0–v0.4.0 cached-token-expiry fixes assume (they tell the user to re-run
// `suiter login`, but offered no CLI way to clear a stale/empty/leaked cached
// token short of hand-editing ~/.suiter/tokens.json). Mirrors newLoginCommand:
// same cobra.ExactArgs(1), same reg.Get(name) "unknown suite" guard, same
// store==nil-at-use guard. Idempotent on a missing entry (exit 0, like `git
// remote remove` on an absent remote) so a dev switching accounts can run it
// without first checking whether a token is cached.
func newLogoutCommand(reg *suite.Registry, store *config.Store) *cobra.Command {
	return &cobra.Command{
		Use:   "logout <suite>",
		Short: "Clear the cached token for a suite",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]
			if _, ok := reg.Get(name); !ok {
				return fmt.Errorf("unknown suite %q (run `suiter suites`)", name)
			}
			if store == nil {
				return fmt.Errorf("%s: token store unavailable — cannot clear", name)
			}
			out := cmd.OutOrStdout()
			// Distinguish "cleared a real entry" from "nothing to clear" so the
			// message is actionable. Delete is a silent no-op on a missing entry,
			// so check presence via Load first (mirrors how the token getter in
			// buildRegistry surfaces "not logged in" on the use path).
			var tok suite.Token
			ok, err := store.Load(name, &tok)
			if err != nil {
				return err
			}
			if !ok {
				fmt.Fprintf(out, "%s: not logged in\n", name)
				return nil
			}
			if err := store.Delete(name); err != nil {
				return err
			}
			fmt.Fprintf(out, "%s: token cleared (%s)\n", name, store.Path())
			return nil
		},
	}
}
