package main

import (
	"encoding/json"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/panbotka/kukatko/internal/ctl"
)

// memberRoleHelp names the roles a member command accepts.
const memberRoleHelp = "viewer, curator, editor or admin"

// newCtlMembersCmd builds the "ctl members" tree: a project admin's management
// of the members of one project (internal/memberapi). The project is the
// invocation's own — the persistent --project, KUKATKO_PROJECT or the
// context's — and every command needs the token of that project's admin (or a
// system administrator's).
func newCtlMembersCmd(opts *ctlOptions) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "members",
		Short: "Manage the members of the project (project admin)",
	}
	cmd.AddCommand(newCtlMembersListCmd(opts), newCtlMembersAddCmd(opts),
		newCtlMembersRoleCmd(opts), newCtlMembersApproveCmd(opts), newCtlMembersRemoveCmd(opts))
	return cmd
}

// newCtlMembersListCmd builds "ctl members list".
func newCtlMembersListCmd(opts *ctlOptions) *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List the project's members with role, approval and last sign-in",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			client, out, err := opts.resolve()
			if err != nil {
				return err
			}
			raw, err := client.ListMembers(cmd.Context())
			if err != nil {
				return fmt.Errorf("listing the members of %s: %w", client.Project(), err)
			}
			return renderRaw(cmd.OutOrStdout(), out, raw, "member list", ctl.DecodeMembers, ctl.WriteMembers)
		},
	}
}

// newCtlMembersAddCmd builds "ctl members add".
func newCtlMembersAddCmd(opts *ctlOptions) *cobra.Command {
	var role string
	cmd := &cobra.Command{
		Use:   "add <username-or-email>",
		Short: "Add an existing account to the project by its exact username or e-mail",
		Long: "Add an existing account to the project, approved at once. The account is\n" +
			"named exactly — its username, or its e-mail address (case-insensitive); nothing\n" +
			"is matched partially. The server answers the same whether the account was\n" +
			"added, already was a member or does not exist, so check `members list`.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, out, err := opts.resolve()
			if err != nil {
				return err
			}
			raw, err := client.AddMember(cmd.Context(), args[0], role)
			if err != nil {
				return fmt.Errorf("adding %s to %s: %w", args[0], client.Project(), err)
			}
			if out.Format == ctl.FormatJSON {
				return writeRawJSON(cmd.OutOrStdout(), raw)
			}
			return renderAck(cmd.OutOrStdout(), out,
				fmt.Sprintf("accepted: if %s names an account, it is a member of %s", args[0], client.Project()))
		},
	}
	cmd.Flags().StringVar(&role, "role", "", "the member's role: "+memberRoleHelp)
	_ = cmd.MarkFlagRequired("role") // the flag exists; the error only names a missing one
	return cmd
}

// newCtlMembersRoleCmd builds "ctl members role".
func newCtlMembersRoleCmd(opts *ctlOptions) *cobra.Command {
	return &cobra.Command{
		Use:   "role <username> <role>",
		Short: "Change a member's role (" + memberRoleHelp + "); never your own",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runMemberChange(cmd, opts, args[0], func(c *ctl.Client, uid string) (json.RawMessage, error) {
				return c.SetMemberRole(cmd.Context(), uid, args[1])
			})
		},
	}
}

// newCtlMembersApproveCmd builds "ctl members approve".
func newCtlMembersApproveCmd(opts *ctlOptions) *cobra.Command {
	return &cobra.Command{
		Use:   "approve <username>",
		Short: "Let a waiting member in; they are mailed a link to the project",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runMemberChange(cmd, opts, args[0], func(c *ctl.Client, uid string) (json.RawMessage, error) {
				return c.ApproveMember(cmd.Context(), uid)
			})
		},
	}
}

// newCtlMembersRemoveCmd builds "ctl members remove".
func newCtlMembersRemoveCmd(opts *ctlOptions) *cobra.Command {
	return &cobra.Command{
		Use:   "remove <username>",
		Short: "Remove a member from the project; the account itself stays",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, out, err := opts.resolve()
			if err != nil {
				return err
			}
			member, err := client.ResolveMember(cmd.Context(), args[0])
			if err != nil {
				return fmt.Errorf("finding member %s: %w", args[0], err)
			}
			if err := client.RemoveMember(cmd.Context(), member.UserUID); err != nil {
				return fmt.Errorf("removing %s from %s: %w", args[0], client.Project(), err)
			}
			return renderAck(cmd.OutOrStdout(), out, fmt.Sprintf("removed %s from %s", args[0], client.Project()))
		},
	}
}

// runMemberChange resolves the member username in the project, applies change
// to its uid and renders the member as it now stands.
func runMemberChange(
	cmd *cobra.Command, opts *ctlOptions, username string,
	change func(*ctl.Client, string) (json.RawMessage, error),
) error {
	client, out, err := opts.resolve()
	if err != nil {
		return err
	}
	member, err := client.ResolveMember(cmd.Context(), username)
	if err != nil {
		return fmt.Errorf("finding member %s: %w", username, err)
	}
	raw, err := change(client, member.UserUID)
	if err != nil {
		return fmt.Errorf("changing member %s of %s: %w", username, client.Project(), err)
	}
	return renderRaw(cmd.OutOrStdout(), out, raw, "member", ctl.DecodeMember, ctl.WriteMember)
}
