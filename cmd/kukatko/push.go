package main

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/panbotka/kukatko/internal/config"
	"github.com/panbotka/kukatko/internal/push"
	"github.com/panbotka/kukatko/internal/pushjob"
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

// buildPushService assembles the push-delivery service — the `push_send` job
// handler that sends a queued notification to one subscribed browser and prunes
// the dead ones — over store.
//
// Unlike mail it is built, and its handler registered, even with push switched
// off: push.New then hands back the no-op sender without reading a key, and the
// handler completes any job left over from a period when push was on without
// sending it, so an operator turning push off drains the queue instead of
// leaving jobs nothing will ever claim. An enabled section with a bad key pair
// is an error, so the start fails rather than every notification.
func buildPushService(cfg *config.Config, store pushjob.SubscriptionStore) (*pushjob.Service, error) {
	sender, err := push.New(push.Config{
		Enabled:    cfg.Push.Enabled,
		PublicKey:  cfg.Push.VAPID.PublicKey,
		PrivateKey: cfg.Push.VAPID.PrivateKey,
		Subject:    cfg.Push.VAPID.Subject,
	})
	if err != nil {
		return nil, fmt.Errorf("building the push sender: %w", err)
	}
	return pushjob.NewService(pushjob.ServiceConfig{
		Enabled: cfg.Push.Enabled, Sender: sender, Store: store,
	}), nil
}
