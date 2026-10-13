package main

import (
	"encoding/json"
	"fmt"
	"io"

	"github.com/spf13/cobra"

	"github.com/panbotka/kukatko/internal/ctl"
)

// newCtlProjectsCmd builds the "ctl projects" tree: the system administrator's
// management of the instance's projects (internal/projectapi). Every command
// needs a maintainer's token; a project is named by its slug.
func newCtlProjectsCmd(opts *ctlOptions) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "projects",
		Short: "Manage the instance's projects (system administrator)",
	}
	cmd.AddCommand(newCtlProjectsListCmd(opts), newCtlProjectsGetCmd(opts), newCtlProjectsCreateCmd(opts),
		newCtlProjectsUpdateCmd(opts), newCtlProjectsActiveCmd(opts, false), newCtlProjectsActiveCmd(opts, true),
		newCtlProjectsAddMemberCmd(opts), newCtlProjectsRekeyCmd(opts))
	return cmd
}

// newCtlProjectsListCmd builds "ctl projects list".
func newCtlProjectsListCmd(opts *ctlOptions) *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List every project with its members, photos, originals and limits",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			client, out, err := opts.resolve()
			if err != nil {
				return err
			}
			raw, err := client.ListProjects(cmd.Context())
			if err != nil {
				return fmt.Errorf("listing projects: %w", err)
			}
			return renderRaw(cmd.OutOrStdout(), out, raw, "project list", ctl.DecodeProjects, ctl.WriteProjects)
		},
	}
}

// newCtlProjectsGetCmd builds "ctl projects get".
func newCtlProjectsGetCmd(opts *ctlOptions) *cobra.Command {
	return &cobra.Command{
		Use:   "get <slug>",
		Short: "Show one project",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, out, err := opts.resolve()
			if err != nil {
				return err
			}
			found, err := client.ResolveProject(cmd.Context(), args[0])
			if err != nil {
				return fmt.Errorf("finding project %s: %w", args[0], err)
			}
			raw, err := client.GetProject(cmd.Context(), found.UID)
			if err != nil {
				return fmt.Errorf("reading project %s: %w", args[0], err)
			}
			return renderProject(cmd.OutOrStdout(), out, raw)
		},
	}
}

// newCtlProjectsCreateCmd builds "ctl projects create".
func newCtlProjectsCreateCmd(opts *ctlOptions) *cobra.Command {
	var in ctl.ProjectInput
	var maxFile, maxTotal int64
	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create a project; you become its first admin",
		Long: "Create a project. The slug is its URL (/p/<slug>/) and can never change:\n" +
			"1–40 lowercase letters, digits or hyphens, not starting with a hyphen.\n" +
			"Limits are in bytes; leave one out for no limit.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			client, out, err := opts.resolve()
			if err != nil {
				return err
			}
			in.MaxFileBytes = changedInt64(cmd, "max-file-bytes", maxFile)
			in.MaxTotalBytes = changedInt64(cmd, "max-total-bytes", maxTotal)
			raw, err := client.CreateProject(cmd.Context(), in)
			if err != nil {
				return fmt.Errorf("creating project %s: %w", in.Slug, err)
			}
			return renderProject(cmd.OutOrStdout(), out, raw)
		},
	}
	flags := cmd.Flags()
	flags.StringVar(&in.Slug, "slug", "", "the project's URL segment (immutable)")
	flags.StringVar(&in.Name, "name", "", "the project's name")
	flags.StringVar(&in.Description, "description", "", "free text about the project")
	flags.Int64Var(&maxFile, "max-file-bytes", 0, "the largest original the project accepts, in bytes")
	flags.Int64Var(&maxTotal, "max-total-bytes", 0, "the cap on the sum of the project's originals, in bytes")
	_ = cmd.MarkFlagRequired("slug") // the flags exist; the error only names a missing one
	_ = cmd.MarkFlagRequired("name")
	return cmd
}

// projectUpdateFlags are the flags of "ctl projects update".
type projectUpdateFlags struct {
	name, description           string
	maxFile, maxTotal           int64
	clearMaxFile, clearMaxTotal bool
}

// patch builds the update from the flags the command line actually set.
func (f *projectUpdateFlags) patch(cmd *cobra.Command) ctl.ProjectPatch {
	patch := ctl.ProjectPatch{
		MaxFileBytes:       changedInt64(cmd, "max-file-bytes", f.maxFile),
		MaxTotalBytes:      changedInt64(cmd, "max-total-bytes", f.maxTotal),
		ClearMaxFileBytes:  f.clearMaxFile,
		ClearMaxTotalBytes: f.clearMaxTotal,
	}
	if cmd.Flags().Changed("name") {
		patch.Name = &f.name
	}
	if cmd.Flags().Changed("description") {
		patch.Description = &f.description
	}
	return patch
}

// newCtlProjectsUpdateCmd builds "ctl projects update".
func newCtlProjectsUpdateCmd(opts *ctlOptions) *cobra.Command {
	var f projectUpdateFlags
	cmd := &cobra.Command{
		Use:   "update <slug>",
		Short: "Change a project's name, description or limits",
		Long: "Change a project. Only the flags you pass change; the slug never does.\n" +
			"--clear-max-file-bytes / --clear-max-total-bytes remove a limit.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runProjectUpdate(cmd, opts, args[0], f.patch(cmd))
		},
	}
	flags := cmd.Flags()
	flags.StringVar(&f.name, "name", "", "the new name")
	flags.StringVar(&f.description, "description", "", "the new description (\"\" clears it)")
	flags.Int64Var(&f.maxFile, "max-file-bytes", 0, "the largest original the project accepts, in bytes")
	flags.Int64Var(&f.maxTotal, "max-total-bytes", 0, "the cap on the sum of the project's originals, in bytes")
	flags.BoolVar(&f.clearMaxFile, "clear-max-file-bytes", false, "remove the per-file limit")
	flags.BoolVar(&f.clearMaxTotal, "clear-max-total-bytes", false, "remove the total quota")
	return cmd
}

// newCtlProjectsActiveCmd builds "ctl projects activate" (active true) or
// "ctl projects deactivate" (active false).
func newCtlProjectsActiveCmd(opts *ctlOptions, active bool) *cobra.Command {
	use, short := "deactivate", "Deactivate a project: nobody can enter it, its data stays"
	if active {
		use, short = "activate", "Reactivate a deactivated project"
	}
	return &cobra.Command{
		Use:   use + " <slug>",
		Short: short,
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runProjectUpdate(cmd, opts, args[0], ctl.ProjectPatch{Active: &active})
		},
	}
}

// runProjectUpdate resolves the project slug and applies patch to it.
func runProjectUpdate(cmd *cobra.Command, opts *ctlOptions, slug string, patch ctl.ProjectPatch) error {
	client, out, err := opts.resolve()
	if err != nil {
		return err
	}
	found, err := client.ResolveProject(cmd.Context(), slug)
	if err != nil {
		return fmt.Errorf("finding project %s: %w", slug, err)
	}
	raw, err := client.UpdateProject(cmd.Context(), found.UID, patch)
	if err != nil {
		return fmt.Errorf("updating project %s: %w", slug, err)
	}
	return renderProject(cmd.OutOrStdout(), out, raw)
}

// newCtlProjectsAddMemberCmd builds "ctl projects add-member".
func newCtlProjectsAddMemberCmd(opts *ctlOptions) *cobra.Command {
	var role string
	cmd := &cobra.Command{
		Use:   "add-member <slug> <username>",
		Short: "Add an account to a project, approved at once",
		Long: "Add an existing account to a project with a role (viewer, curator, editor or\n" +
			"admin). The membership is approved at once. Pass your own username to enter a\n" +
			"project yourself.",
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runProjectAddMember(cmd, opts, args[0], args[1], role)
		},
	}
	cmd.Flags().StringVar(&role, "role", "", "the member's role: viewer, curator, editor or admin")
	_ = cmd.MarkFlagRequired("role") // the flag exists; the error only names a missing one
	return cmd
}

// runProjectAddMember resolves the project and the account and adds the
// membership, reporting it by name.
func runProjectAddMember(cmd *cobra.Command, opts *ctlOptions, slug, username, role string) error {
	client, out, err := opts.resolve()
	if err != nil {
		return err
	}
	found, err := client.ResolveProject(cmd.Context(), slug)
	if err != nil {
		return fmt.Errorf("finding project %s: %w", slug, err)
	}
	account, err := client.ResolveAccount(cmd.Context(), username)
	if err != nil {
		return fmt.Errorf("finding account %s: %w", username, err)
	}
	raw, err := client.AddProjectMember(cmd.Context(), found.UID, account.UID, role)
	if err != nil {
		return fmt.Errorf("adding %s to %s: %w", username, slug, err)
	}
	switch out.Format {
	case ctl.FormatJSON:
		return writeRawJSON(cmd.OutOrStdout(), raw)
	case ctl.FormatLLM:
		if err := ctl.WriteLLM(cmd.OutOrStdout(), raw, out.Fields); err != nil {
			return fmt.Errorf("rendering the membership: %w", err)
		}
		return nil
	default:
		return renderAck(cmd.OutOrStdout(), out, fmt.Sprintf("added %s to %s as %s", username, slug, role))
	}
}

// newCtlUsersCmd builds the "ctl users" tree: every account of the instance
// and the system administrator role. Every command needs a maintainer's token.
func newCtlUsersCmd(opts *ctlOptions) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "users",
		Short: "List the instance's accounts, promote or demote a system administrator",
	}
	cmd.AddCommand(newCtlUsersListCmd(opts), newCtlUsersRoleCmd(opts, true), newCtlUsersRoleCmd(opts, false))
	return cmd
}

// newCtlUsersListCmd builds "ctl users list".
func newCtlUsersListCmd(opts *ctlOptions) *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List every account with its instance role and project memberships",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			client, out, err := opts.resolve()
			if err != nil {
				return err
			}
			raw, err := client.ListAccounts(cmd.Context())
			if err != nil {
				return fmt.Errorf("listing accounts: %w", err)
			}
			return renderRaw(cmd.OutOrStdout(), out, raw, "account list", ctl.DecodeAccounts, ctl.WriteAccounts)
		},
	}
}

// newCtlUsersRoleCmd builds "ctl users promote" (promote true) or
// "ctl users demote".
func newCtlUsersRoleCmd(opts *ctlOptions, promote bool) *cobra.Command {
	use, short, role := "demote", "Make a system administrator a plain user (never yourself)", "user"
	if promote {
		use, short, role = "promote", "Make an account a system administrator", "maintainer"
	}
	return &cobra.Command{
		Use:   use + " <username>",
		Short: short,
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, out, err := opts.resolve()
			if err != nil {
				return err
			}
			account, err := client.ResolveAccount(cmd.Context(), args[0])
			if err != nil {
				return fmt.Errorf("finding account %s: %w", args[0], err)
			}
			raw, err := client.SetInstanceRole(cmd.Context(), account.UID, role)
			if err != nil {
				return fmt.Errorf("setting the instance role of %s: %w", args[0], err)
			}
			return renderRaw(cmd.OutOrStdout(), out, raw, "account", ctl.DecodeAccount, ctl.WriteAccount)
		},
	}
}

// renderProject writes one project.
func renderProject(w io.Writer, out ctl.Output, raw json.RawMessage) error {
	return renderRaw(w, out, raw, "project", ctl.DecodeProject, ctl.WriteProject)
}

// changedInt64 returns &value when the command line set the flag name, nil
// otherwise — so "not given" and "given as 0" stay apart and the server, not
// the CLI, refuses a zero limit.
func changedInt64(cmd *cobra.Command, name string, value int64) *int64 {
	if !cmd.Flags().Changed(name) {
		return nil
	}
	return &value
}
