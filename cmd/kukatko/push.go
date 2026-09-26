package main

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/panbotka/kukatko/internal/push"
)

// pushKeysReminder is printed on stderr after a key pair, so redirecting stdout
// into an env file captures the keys and nothing else.
const pushKeysReminder = `
The private key is a secret. Put it in the environment of the server
(KUKATKO_PUSH_VAPID_PRIVATE_KEY, kept in the password manager) and never in a
committed file. The public key may go into config.yaml as push.vapid.public_key.
Set push.vapid.subject to a mailto: or an https: URL and push.enabled to true.

Mint a pair once per instance: every browser subscribes against the public key,
so replacing the pair invalidates every existing subscription.
`

// newPushCmd builds the "push" command group for Web Push administration. It
// has one child today, generate-keys.
func newPushCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "push",
		Short: "Web Push notification administration",
		Args:  cobra.NoArgs,
	}
	cmd.AddCommand(newPushGenerateKeysCmd())
	return cmd
}

// newPushGenerateKeysCmd builds "push generate-keys", which mints a fresh VAPID
// key pair and prints it as the two environment assignments a deployment needs.
// It reads no configuration and touches no database, so it runs anywhere the
// binary does. The keys go to stdout, the reminder about keeping the private key
// secret to stderr.
func newPushGenerateKeysCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "generate-keys",
		Short: "Generate a VAPID key pair for Web Push",
		Long: "Generates a fresh VAPID (P-256) key pair and prints it as environment " +
			"assignments. The private key belongs in the environment, never in a committed file.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			keys, err := push.GenerateKeys()
			if err != nil {
				return fmt.Errorf("generating the VAPID key pair: %w", err)
			}
			out := cmd.OutOrStdout()
			if _, err := fmt.Fprintf(out, "KUKATKO_PUSH_VAPID_PUBLIC_KEY=%s\nKUKATKO_PUSH_VAPID_PRIVATE_KEY=%s\n",
				keys.PublicKey, keys.PrivateKey); err != nil {
				return fmt.Errorf("printing the key pair: %w", err)
			}
			cmd.PrintErr(pushKeysReminder)
			return nil
		},
	}
}
