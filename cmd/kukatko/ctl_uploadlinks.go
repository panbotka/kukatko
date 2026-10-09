package main

import (
	"encoding/json"
	"fmt"
	"io"

	"github.com/spf13/cobra"

	"github.com/panbotka/kukatko/internal/ctl"
)

// newCtlUploadLinksCmd builds the "ctl upload-links" tree: the short links
// anybody uploads event photos through (`internal/uploadlinkapi`).
//
// Every command needs a curator token, and a link is visible and manageable
// only to its creator or an admin — anybody else's is a 404. Creating,
// extending and revoking stay in the web UI; the CLI covers what an operator
// needs to hand a link out again: its URL, the restore of a pre-0091 link's
// original code, and a fresh code for a link that leaked.
func newCtlUploadLinksCmd(opts *ctlOptions) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "upload-links",
		Short: "List upload links with their URLs, restore a link's code or give it a new one",
	}
	cmd.AddCommand(newCtlUploadLinksListCmd(opts), newCtlUploadLinksRestoreCodeCmd(opts),
		newCtlUploadLinksNewCodeCmd(opts))
	return cmd
}

// newCtlUploadLinksListCmd builds "ctl upload-links list".
func newCtlUploadLinksListCmd(opts *ctlOptions) *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List your upload links (an admin's: all) with their URLs, newest first",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			client, out, err := opts.resolve()
			if err != nil {
				return err
			}
			raw, err := client.ListUploadLinks(cmd.Context())
			if err != nil {
				return fmt.Errorf("listing upload links: %w", err)
			}
			return renderUploadLinks(cmd.OutOrStdout(), out, client.Server(), raw)
		},
	}
}

// newCtlUploadLinksRestoreCodeCmd builds "ctl upload-links restore-code".
func newCtlUploadLinksRestoreCodeCmd(opts *ctlOptions) *cobra.Command {
	return &cobra.Command{
		Use:   "restore-code <uid> <code-or-url>",
		Short: "Make a link's original code readable again; its URL does not change",
		Long: "Restore the code of a link created before codes were stored readably.\n\n" +
			"Such a link still works, but its URL can no longer be shown. Give the original\n" +
			"code (or the whole /u/<code> link): the server stores it only when it matches\n" +
			"the hash the link already holds, so nothing about the link changes except that\n" +
			"its URL can be listed and copied again. A wrong code is refused.",
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, out, err := opts.resolve()
			if err != nil {
				return err
			}
			raw, err := client.RestoreUploadLinkCode(cmd.Context(), args[0], args[1])
			if err != nil {
				return fmt.Errorf("restoring the code of upload link %s: %w", args[0], err)
			}
			return renderUploadLink(cmd.OutOrStdout(), out, client.Server(), raw)
		},
	}
}

// newCtlUploadLinksNewCodeCmd builds "ctl upload-links new-code", behind the
// irreversible gate: the old URL dies.
func newCtlUploadLinksNewCodeCmd(opts *ctlOptions) *cobra.Command {
	var assumeYes, dryRun bool
	cmd := &cobra.Command{
		Use:   "new-code <uid>",
		Short: "Give a link a fresh code; the old URL stops working at once",
		Long: "Replace a link's code, for a link whose URL leaked.\n\n" +
			"The old URL stops working immediately — everybody who has it must be sent the\n" +
			"new one. Albums, labels, expiry and the photos already uploaded stay as they\n" +
			"are. Pass --yes to confirm, or --dry-run to see which link would change.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runUploadLinkNewCode(cmd, opts, args[0], assumeYes, dryRun)
		},
	}
	addIrreversibleFlags(cmd, &assumeYes, &dryRun)
	return cmd
}

// runUploadLinkNewCode names the link before replacing its code, so the
// confirmation says which link's URL dies rather than which uid's.
func runUploadLinkNewCode(cmd *cobra.Command, opts *ctlOptions, uid string, assumeYes, dryRun bool) error {
	client, out, err := opts.resolve()
	if err != nil {
		return err
	}
	raw, err := client.ListUploadLinks(cmd.Context())
	if err != nil {
		return fmt.Errorf("listing upload links: %w", err)
	}
	links, err := ctl.DecodeUploadLinks(raw)
	if err != nil {
		return fmt.Errorf("reading upload links: %w", err)
	}
	name := uid
	for _, link := range links {
		if link.UID == uid {
			name = ctl.NamedUID(link.Title, link.UID)
		}
	}
	action := "replace the code of upload link " + name + " (its current URL stops working)"
	if dryRun {
		return renderAck(cmd.OutOrStdout(), out, "dry run: would "+action+"; nothing was changed")
	}
	if err := confirmIrreversible(assumeYes, action); err != nil {
		return err
	}
	raw, err = client.NewUploadLinkCode(cmd.Context(), uid)
	if err != nil {
		return fmt.Errorf("replacing the code of upload link %s: %w", uid, err)
	}
	return renderUploadLink(cmd.OutOrStdout(), out, client.Server(), raw)
}

// renderUploadLinks writes the upload link list, URLs made absolute on server.
func renderUploadLinks(w io.Writer, out ctl.Output, server string, raw json.RawMessage) error {
	return renderRaw(w, out, raw, "upload link list", ctl.DecodeUploadLinks,
		func(w io.Writer, links []ctl.UploadLink) error { return ctl.WriteUploadLinks(w, server, links) })
}

// renderUploadLink writes one upload link, its URL made absolute on server.
func renderUploadLink(w io.Writer, out ctl.Output, server string, raw json.RawMessage) error {
	return renderRaw(w, out, raw, "upload link", ctl.DecodeUploadLink,
		func(w io.Writer, link ctl.UploadLink) error { return ctl.WriteUploadLink(w, server, link) })
}
