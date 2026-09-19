package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/panbotka/kukatko/internal/ctl"
)

// newCtlTasksCmd assembles the task command group: the work queue over the
// library, and the thread that answers it.
func newCtlTasksCmd(opts *ctlOptions) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "tasks",
		Aliases: []string{"task"},
		Short:   "Open, answer and close the questions a batch of curation ends at",
		Long: "The work queue: a question about a frozen group of photographs.\n\n" +
			"A task holds the question, the photographs it is about, the state of play\n" +
			"and the conversation that settles it. Reading one and writing in its thread\n" +
			"are open to every signed-in role; opening and closing need write access.\n\n" +
			"`tasks list --answered` is the one an agent polls: it lists the tasks\n" +
			"somebody has replied to since the state last moved — the work that has an\n" +
			"answer and is waiting to be written into the library.",
	}
	cmd.AddCommand(
		newCtlTasksListCmd(opts), newCtlTasksShowCmd(opts), newCtlTasksCreateCmd(opts),
		newCtlTasksUpdateCmd(opts), newCtlTasksDeleteCmd(opts),
		newCtlTasksAddPhotosCmd(opts), newCtlTasksRemovePhotosCmd(opts),
		newCtlTasksCommentsCmd(opts), newCtlTasksCommentCmd(opts),
		newCtlTasksParticipantsCmd(opts), newCtlTasksAssignCmd(opts),
		newCtlTasksUnassignCmd(opts),
	)
	return cmd
}

// newCtlTasksListCmd lists tasks, narrowed by state or by whether anybody has
// answered.
func newCtlTasksListCmd(opts *ctlOptions) *cobra.Command {
	var listOpts ctl.TaskListOptions
	var states string
	var mine bool
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List tasks, open ones first",
		Long: "List tasks.\n\n" +
			"Open tasks come first, the most recently touched at the top, where a reply\n" +
			"counts as a touch. The NEW column marks a task somebody has answered since\n" +
			"its state last moved; --answered lists only those.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			listOpts.States = splitStates(states)
			if mine {
				listOpts.Participant = "me"
			}
			client, out, err := opts.resolve()
			if err != nil {
				return err
			}
			raw, err := client.ListTasks(cmd.Context(), listOpts)
			if err != nil {
				return fmt.Errorf("listing tasks: %w", err)
			}
			return renderTaskPage(cmd.OutOrStdout(), out, raw)
		},
	}
	flags := cmd.Flags()
	flags.StringVar(&states, "state", "",
		"only these states, comma separated (question, working, review, done, rejected)")
	flags.BoolVar(&listOpts.Open, "open", false, "only the states a live task can be in")
	flags.BoolVar(&listOpts.Answered, "answered", false,
		"only tasks answered since the state last moved")
	flags.StringVar(&listOpts.Search, "search", "", "match a substring of the question or its context")
	flags.StringVar(&listOpts.PhotoUID, "photo", "", "only tasks this photo is part of")
	flags.StringVar(&listOpts.Participant, "participant", "",
		`only tasks this person is on; "me" means whoever the token belongs to`)
	flags.BoolVar(&mine, "mine", false, `shorthand for --participant me`)
	flags.IntVar(&listOpts.Limit, "limit", 0, "how many to return")
	flags.IntVar(&listOpts.Offset, "offset", 0, "where to start")
	return cmd
}

// splitStates turns the comma-separated flag into the list the client sends,
// dropping the empty pieces a trailing comma leaves behind.
func splitStates(raw string) []string {
	var out []string
	for piece := range strings.SplitSeq(raw, ",") {
		if trimmed := strings.TrimSpace(piece); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}

// newCtlTasksShowCmd prints one task in full.
func newCtlTasksShowCmd(opts *ctlOptions) *cobra.Command {
	return &cobra.Command{
		Use:   "show <uid>",
		Short: "Print one task: the question, the state of play and the context",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, out, err := opts.resolve()
			if err != nil {
				return err
			}
			raw, err := client.GetTask(cmd.Context(), args[0])
			if err != nil {
				return fmt.Errorf("fetching task %s: %w", args[0], err)
			}
			return renderTask(cmd.OutOrStdout(), out, raw)
		},
	}
}

// newCtlTasksCreateCmd opens a task over the photographs named on the command
// line or read from stdin.
func newCtlTasksCreateCmd(opts *ctlOptions) *cobra.Command {
	var in ctl.TaskInput
	var bodyFile string
	cmd := &cobra.Command{
		Use:   "create [<photo-uid>…]",
		Short: "Open a task over a group of photographs",
		Long: "Open a task.\n\n" +
			"The photographs are given as arguments or read from stdin, one uid per line,\n" +
			"and the group is frozen: it is the record of what the batch was about, so it\n" +
			"does not follow a query as the data is fixed. Pass the query that produced it\n" +
			"as --query and it is kept verbatim as evidence, never re-run.",
		RunE: func(cmd *cobra.Command, args []string) error {
			body, err := textOrFile(cmd, "body", in.Body, bodyFile)
			if err != nil {
				return err
			}
			in.Body = body
			uids, _, err := photoUIDsFromArgs(cmd, args)
			if err != nil {
				return err
			}
			in.PhotoUIDs = uids
			client, out, err := opts.resolve()
			if err != nil {
				return err
			}
			raw, err := client.CreateTask(cmd.Context(), in)
			if err != nil {
				return fmt.Errorf("creating task: %w", err)
			}
			return renderTask(cmd.OutOrStdout(), out, raw)
		},
	}
	flags := cmd.Flags()
	flags.StringVar(&in.Title, "title", "", "the question, in one line (required)")
	flags.StringVar(&in.Body, "body", "", "the context, in Markdown")
	flags.StringVar(&bodyFile, "body-file", "", "read the context from this file instead of --body")
	flags.StringVar(&in.Query, "query", "", "the search that produced the group, kept as evidence")
	flags.StringVar(&in.State, "state", "", "the state to open in (default question)")
	return cmd
}

// newCtlTasksUpdateCmd edits a task or advances its state.
func newCtlTasksUpdateCmd(opts *ctlOptions) *cobra.Command {
	var title, body, bodyFile, searchQuery, state, resolution, resolutionFile string
	cmd := &cobra.Command{
		Use:   "update <uid>",
		Short: "Edit a task, or advance its state",
		Long: "Edit a task.\n\n" +
			"Only the flags you give are changed; an explicit empty value clears a field.\n" +
			"Closing a task (--state done or --state rejected) needs a --resolution: a\n" +
			"closed task always says how it ended, and \"rejected\" is a result rather than\n" +
			"a failure. Reopening one clears the closing marks but keeps the text.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			upd, err := taskUpdateFromFlags(cmd, updateFlags{
				title: title, body: body, bodyFile: bodyFile, query: searchQuery,
				state: state, resolution: resolution, resolutionFile: resolutionFile,
			})
			if err != nil {
				return err
			}
			client, out, err := opts.resolve()
			if err != nil {
				return err
			}
			raw, err := client.UpdateTask(cmd.Context(), args[0], upd)
			if err != nil {
				return fmt.Errorf("updating task %s: %w", args[0], err)
			}
			return renderTask(cmd.OutOrStdout(), out, raw)
		},
	}
	flags := cmd.Flags()
	flags.StringVar(&title, "title", "", "rewrite the question")
	flags.StringVar(&body, "body", "", "rewrite the context")
	flags.StringVar(&bodyFile, "body-file", "", "read the context from this file instead of --body")
	flags.StringVar(&searchQuery, "query", "", "rewrite the remembered search")
	flags.StringVar(&state, "state", "", "move to this state")
	flags.StringVar(&resolution, "resolution", "", "how it ended (required when closing)")
	flags.StringVar(&resolutionFile, "resolution-file", "",
		"read the resolution from this file instead of --resolution")
	return cmd
}

// updateFlags carries the raw flag values of the update command, so building the
// partial update stays one short function.
type updateFlags struct {
	title          string
	body           string
	bodyFile       string
	query          string
	state          string
	resolution     string
	resolutionFile string
}

// taskUpdateFromFlags builds the partial update, treating an unset flag as
// "leave this alone" and a file flag as the value of its sibling.
func taskUpdateFromFlags(cmd *cobra.Command, f updateFlags) (ctl.TaskUpdate, error) {
	body, err := textOrFile(cmd, "body", f.body, f.bodyFile)
	if err != nil {
		return ctl.TaskUpdate{}, err
	}
	resolution, err := textOrFile(cmd, "resolution", f.resolution, f.resolutionFile)
	if err != nil {
		return ctl.TaskUpdate{}, err
	}
	return ctl.TaskUpdate{
		Title:      optionalString(cmd, "title", f.title),
		Body:       optionalText(cmd, "body", f.bodyFile, body),
		Query:      optionalString(cmd, "query", f.query),
		State:      optionalString(cmd, "state", f.state),
		Resolution: optionalText(cmd, "resolution", f.resolutionFile, resolution),
	}, nil
}

// optionalText is optionalString for a field that may also have been given as a
// file: either flag counts as "the caller named this field".
func optionalText(cmd *cobra.Command, name, fileFlag, value string) *string {
	if fileFlag != "" || cmd.Flags().Changed(name) {
		return &value
	}
	return nil
}

// textOrFile returns the flag's value, or the contents of the file named by its
// sibling. Giving both is refused rather than silently preferring one.
func textOrFile(cmd *cobra.Command, name, value, path string) (string, error) {
	if path == "" {
		return value, nil
	}
	if cmd.Flags().Changed(name) {
		return "", fmt.Errorf("give either --%s or --%s-file, not both", name, name)
	}
	// The path comes from the operator's own command line, which is as trusted as
	// the rest of it.
	content, err := os.ReadFile(path) //nolint:gosec // operator-supplied path
	if err != nil {
		return "", fmt.Errorf("reading --%s-file: %w", name, err)
	}
	return string(content), nil
}

// newCtlTasksDeleteCmd deletes a task outright, behind the irreversible gate.
func newCtlTasksDeleteCmd(opts *ctlOptions) *cobra.Command {
	var assumeYes, dryRun bool
	cmd := &cobra.Command{
		Use:   "delete <uid>",
		Short: "Delete a task, its membership and its thread (editor or admin)",
		Long: "Delete a task.\n\n" +
			"The photographs survive untouched, but the record goes: which photographs a\n" +
			"batch of edits was about, the question, and every answer written under it.\n" +
			"Finished work is normally closed rather than deleted, exactly so that record\n" +
			"survives. Pass --yes to confirm, or --dry-run to see which task would go.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runTaskDelete(cmd, opts, args[0], assumeYes, dryRun)
		},
	}
	addIrreversibleFlags(cmd, &assumeYes, &dryRun)
	return cmd
}

// runTaskDelete names the task before removing it, so the confirmation says which
// question went rather than which uid did.
func runTaskDelete(cmd *cobra.Command, opts *ctlOptions, uid string, assumeYes, dryRun bool) error {
	client, out, err := opts.resolve()
	if err != nil {
		return err
	}
	raw, err := client.GetTask(cmd.Context(), uid)
	if err != nil {
		return fmt.Errorf("fetching task %s: %w", uid, err)
	}
	task, err := ctl.DecodeTask(raw)
	if err != nil {
		return fmt.Errorf("reading task %s: %w", uid, err)
	}
	action := fmt.Sprintf("delete task %s (%d photos, %d comments)",
		ctl.NamedUID(task.Title, task.UID), task.PhotoCount, task.CommentCount)
	if dryRun {
		return renderAck(cmd.OutOrStdout(), out,
			"dry run: would "+action+"; nothing was changed")
	}
	if err := confirmIrreversible(assumeYes, action); err != nil {
		return err
	}
	if err := client.DeleteTask(cmd.Context(), uid); err != nil {
		return fmt.Errorf("deleting task %s: %w", uid, err)
	}
	return renderAck(cmd.OutOrStdout(), out, "task deleted; the photographs are untouched")
}

// newCtlTasksAddPhotosCmd adds photographs to a task's group.
func newCtlTasksAddPhotosCmd(opts *ctlOptions) *cobra.Command {
	return newCtlTaskMembershipCmd(opts, membershipVerb{
		use: "add-photos <uid> [<photo-uid>…]", short: "Add photographs to a task",
		gerund: "adding photos to task",
		change: func(c *ctl.Client, cmd *cobra.Command, uid string, uids []string) (json.RawMessage, error) {
			return c.AddTaskPhotos(cmd.Context(), uid, uids)
		},
	})
}

// newCtlTasksRemovePhotosCmd drops photographs from a task's group.
func newCtlTasksRemovePhotosCmd(opts *ctlOptions) *cobra.Command {
	return newCtlTaskMembershipCmd(opts, membershipVerb{
		use: "remove-photos <uid> [<photo-uid>…]", short: "Remove photographs from a task",
		gerund: "removing photos from task",
		change: func(c *ctl.Client, cmd *cobra.Command, uid string, uids []string) (json.RawMessage, error) {
			return c.RemoveTaskPhotos(cmd.Context(), uid, uids)
		},
	})
}

// membershipVerb is the difference between the two membership commands: the
// words, and which client call they make.
type membershipVerb struct {
	use    string
	short  string
	gerund string
	change func(*ctl.Client, *cobra.Command, string, []string) (json.RawMessage, error)
}

// newCtlTaskMembershipCmd builds one of the two membership commands, which are
// otherwise the same command twice.
func newCtlTaskMembershipCmd(opts *ctlOptions, verb membershipVerb) *cobra.Command {
	return &cobra.Command{
		Use:   verb.use,
		Short: verb.short,
		Long: verb.short + ".\n\nThe photographs are given as arguments or read from stdin,\n" +
			"one uid per line. Photographs already in (or already out of) the task are\n" +
			"ignored, so replaying a batch changes nothing.",
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			uids, _, err := photoUIDsFromArgs(cmd, args[1:])
			if err != nil {
				return err
			}
			client, out, err := opts.resolve()
			if err != nil {
				return err
			}
			raw, err := verb.change(client, cmd, args[0], uids)
			if err != nil {
				return fmt.Errorf("%s %s: %w", verb.gerund, args[0], err)
			}
			return renderTaskMembership(cmd.OutOrStdout(), out, raw)
		},
	}
}

// newCtlTasksCommentsCmd prints a task's thread.
func newCtlTasksCommentsCmd(opts *ctlOptions) *cobra.Command {
	return &cobra.Command{
		Use:   "comments <uid>",
		Short: "Print a task's thread — the answers to its question",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, out, err := opts.resolve()
			if err != nil {
				return err
			}
			raw, err := client.ListTaskComments(cmd.Context(), args[0])
			if err != nil {
				return fmt.Errorf("fetching the thread of task %s: %w", args[0], err)
			}
			return renderComments(cmd.OutOrStdout(), out, raw)
		},
	}
}

// newCtlTasksCommentCmd answers a task.
func newCtlTasksCommentCmd(opts *ctlOptions) *cobra.Command {
	var bodyFile string
	cmd := &cobra.Command{
		Use:   "comment <uid> [<text>]",
		Short: "Answer a task, or add a note to its thread",
		Long: "Write in a task's thread.\n\n" +
			"The comment is attributed to the token's own account, always — so an agent\n" +
			"answers as itself and never puts words in a person's mouth, in a thread\n" +
			"whose whole value is that it records who remembered what.",
		Args: cobra.RangeArgs(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			body, err := commentBodyFromArgs(args, bodyFile)
			if err != nil {
				return err
			}
			client, out, err := opts.resolve()
			if err != nil {
				return err
			}
			raw, err := client.AddTaskComment(cmd.Context(), args[0], body)
			if err != nil {
				return fmt.Errorf("commenting on task %s: %w", args[0], err)
			}
			return renderComment(cmd.OutOrStdout(), out, raw)
		},
	}
	cmd.Flags().StringVar(&bodyFile, "body-file", "", "read the comment from this file")
	return cmd
}

// commentBodyFromArgs takes the comment from the command line or from the file,
// refusing both at once and neither at all.
func commentBodyFromArgs(args []string, path string) (string, error) {
	hasText := len(args) > 1
	switch {
	case hasText && path != "":
		return "", errors.New("give either the text or --body-file, not both")
	case hasText:
		return args[1], nil
	case path == "":
		return "", errors.New("give the comment text, or --body-file")
	}
	content, err := os.ReadFile(path) //nolint:gosec // operator-supplied path
	if err != nil {
		return "", fmt.Errorf("reading --body-file: %w", err)
	}
	return string(content), nil
}
