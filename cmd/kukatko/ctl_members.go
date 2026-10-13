package main

import (
	"encoding/json"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/panbotka/kukatko/internal/ctl"
	"github.com/panbotka/kukatko/internal/project"
)

// memberRoleHelp names the roles a member command accepts.
const memberRoleHelp = "viewer, curator, editor or admin"

// newCtlMembersCmd builds the "ctl members" tree: a project admin's management
// of the members of one project (internal/memberapi). The project is named by
// --project (a slug, default the bootstrap project) until ctl contexts carry
// one; every command needs the token of that project's admin (or a system
// administrator's).
func newCtlMembersCmd(opts *ctlOptions) *cobra.Command {
	var slug string
	cmd := &cobra.Command{
		Use:   "members",
		Short: "Manage the members of a project (project admin)",
	}
	cmd.PersistentFlags().StringVar(&slug, "project", project.BootstrapSlug,
		"the project's slug (/p/<slug>/)")
	cmd.AddCommand(newCtlMembersListCmd(opts, &slug), newCtlMembersAddCmd(opts, &slug),
		newCtlMembersRoleCmd(opts, &slug), newCtlMembersApproveCmd(opts, &slug),
		newCtlMembersRemoveCmd(opts, &slug))
	return cmd
}

// newCtlMembersListCmd builds "ctl members list".
func newCtlMembersListCmd(opts *ctlOptions, slug *string) *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List the project's members with role, approval and last sign-in",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			client, out, err := opts.resolve()
			if err != nil {
				return err
			}
			raw, err := client.ListMembers(cmd.Context(), *slug)
			if err != nil {
				return fmt.Errorf("listing the members of %s: %w", *slug, err)
			}
			return renderRaw(cmd.OutOrStdout(), out, raw, "member list", ctl.DecodeMembers, ctl.WriteMembers)
		},
	}
}

// newCtlMembersAddCmd builds "ctl members add".
func newCtlMembersAddCmd(opts *ctlOptions, slug *string) *cobra.Command {
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
			raw, err := client.AddMember(cmd.Context(), *slug, args[0], role)
			if err != nil {
				return fmt.Errorf("adding %s to %s: %w", args[0], *slug, err)
			}
			if out.Format == ctl.FormatJSON {
				return writeRawJSON(cmd.OutOrStdout(), raw)
			}
			return renderAck(cmd.OutOrStdout(), out,
				fmt.Sprintf("accepted: if %s names an account, it is a member of %s", args[0], *slug))
		},
	}
	cmd.Flags().StringVar(&role, "role", "", "the member's role: "+memberRoleHelp)
	_ = cmd.MarkFlagRequired("role") // the flag exists; the error only names a missing one
	return cmd
}

// newCtlMembersRoleCmd builds "ctl members role".
func newCtlMembersRoleCmd(opts *ctlOptions, slug *string) *cobra.Command {
	return &cobra.Command{
		Use:   "role <username> <role>",
		Short: "Change a member's role (" + memberRoleHelp + "); never your own",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runMemberChange(cmd, opts, *slug, args[0], func(c *ctl.Client, uid string) (json.RawMessage, error) {
				return c.SetMemberRole(cmd.Context(), *slug, uid, args[1])
			})
		},
	}
}

// newCtlMembersApproveCmd builds "ctl members approve".
func newCtlMembersApproveCmd(opts *ctlOptions, slug *string) *cobra.Command {
	return &cobra.Command{
		Use:   "approve <username>",
		Short: "Let a waiting member in; they are mailed a link to the project",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runMemberChange(cmd, opts, *slug, args[0], func(c *ctl.Client, uid string) (json.RawMessage, error) {
				return c.ApproveMember(cmd.Context(), *slug, uid)
			})
		},
	}
}

// newCtlMembersRemoveCmd builds "ctl members remove".
func newCtlMembersRemoveCmd(opts *ctlOptions, slug *string) *cobra.Command {
	return &cobra.Command{
		Use:   "remove <username>",
		Short: "Remove a member from the project; the account itself stays",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, out, err := opts.resolve()
			if err != nil {
				return err
			}
			member, err := client.ResolveMember(cmd.Context(), *slug, args[0])
			if err != nil {
				return fmt.Errorf("finding member %s: %w", args[0], err)
			}
			if err := client.RemoveMember(cmd.Context(), *slug, member.UserUID); err != nil {
				return fmt.Errorf("removing %s from %s: %w", args[0], *slug, err)
			}
			return renderAck(cmd.OutOrStdout(), out, fmt.Sprintf("removed %s from %s", args[0], *slug))
		},
	}
}

// runMemberChange resolves the member username in the project slug, applies
// change to its uid and renders the member as it now stands.
func runMemberChange(
	cmd *cobra.Command, opts *ctlOptions, slug, username string,
	change func(*ctl.Client, string) (json.RawMessage, error),
) error {
	client, out, err := opts.resolve()
	if err != nil {
		return err
	}
	member, err := client.ResolveMember(cmd.Context(), slug, username)
	if err != nil {
		return fmt.Errorf("finding member %s: %w", username, err)
	}
	raw, err := change(client, member.UserUID)
	if err != nil {
		return fmt.Errorf("changing member %s of %s: %w", username, slug, err)
	}
	return renderRaw(cmd.OutOrStdout(), out, raw, "member", ctl.DecodeMember, ctl.WriteMember)
}
