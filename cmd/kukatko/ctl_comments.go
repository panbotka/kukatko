package main

import (
	"fmt"
	"strconv"

	"github.com/spf13/cobra"

	"github.com/panbotka/kukatko/internal/ctl"
)

// newCtlCommentsCmd builds the "ctl comments" tree: a photo's conversation
// (`internal/comments`). Every signed-in role may read a thread and write into
// one — a comment is participation, not curation.
//
// **Reading matters most.** A thread is often the only record of who is on a
// photo, where it was taken and when, which is exactly what is needed to date a
// photo nobody wrote a date on.
//
// **Writing happens under the token's own account, always.** The API takes the
// author from the authenticated principal and the audit trail records it there
// too, so commenting through a person's token would put words in that person's
// mouth — in the one place in the library whose whole value is that it says who
// remembered what. That is why the MCP server exposes no comment tool at all;
// `ctl` may have one because an agent's account is a distinct one.
func newCtlCommentsCmd(opts *ctlOptions) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "comments",
		Short: "Read a photo's comment thread and write into it",
	}
	cmd.AddCommand(newCtlCommentsListCmd(opts), newCtlCommentsAddCmd(opts))
	return cmd
}

// newCtlCommentsListCmd builds "ctl comments list <photo-uid>".
func newCtlCommentsListCmd(opts *ctlOptions) *cobra.Command {
	return &cobra.Command{
		Use:   "list <photo-uid>",
		Short: "Read a photo's whole comment thread, oldest first",
		Long: "Read a photo's whole comment thread, oldest first.\n\n" +
			"The bodies are printed in full rather than elided into a column: a comment is\n" +
			"a paragraph somebody wrote, and it is often the only record of who, where and\n" +
			"when. A photo with no thread — and a photo that does not exist — both print\n" +
			"one line saying there are no comments.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, out, err := opts.resolve()
			if err != nil {
				return err
			}
			raw, err := client.ListComments(cmd.Context(), args[0])
			if err != nil {
				return fmt.Errorf("reading the comments of photo %s: %w", args[0], err)
			}
			return renderComments(cmd.OutOrStdout(), out, raw)
		},
	}
}

// newCtlCommentsAddCmd builds "ctl comments add <photo-uid> <text>".
func newCtlCommentsAddCmd(opts *ctlOptions) *cobra.Command {
	return &cobra.Command{
		Use:   "add <photo-uid> <text>",
		Short: "Append a comment to a photo's thread, under your own account",
		Long: "Append a comment to a photo's thread.\n\n" +
			"The author is whoever the API token belongs to — the server takes it from the\n" +
			"authenticated principal and the audit trail records it there too. So write\n" +
			"only under your own account: a comment posted through somebody else's token\n" +
			"puts words in their mouth, in the one place in the library whose value is that\n" +
			"it says who remembered what.\n\n" +
			"The body is plain text, at most " + strconv.Itoa(ctl.MaxCommentLen) + " characters.",
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, out, err := opts.resolve()
			if err != nil {
				return err
			}
			raw, err := client.AddComment(cmd.Context(), args[0], args[1])
			if err != nil {
				return fmt.Errorf("commenting on photo %s: %w", args[0], err)
			}
			return renderComment(cmd.OutOrStdout(), out, raw)
		},
	}
}
