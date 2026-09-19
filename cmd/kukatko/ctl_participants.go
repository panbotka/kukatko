package main

import (
	"encoding/json"
	"fmt"
	"io"

	"github.com/spf13/cobra"

	"github.com/panbotka/kukatko/internal/ctl"
)

// newCtlTasksParticipantsCmd prints the people on a task.
func newCtlTasksParticipantsCmd(opts *ctlOptions) *cobra.Command {
	return &cobra.Command{
		Use:   "participants <uid>",
		Short: "Print who is on a task",
		Long: "Print the people on a task.\n\n" +
			"Most of them are there because they did something to it — opening a task,\n" +
			"answering it or moving it along puts you on it. The HOW column says which:\n" +
			"\"acted\" for those, and \"asked by …\" for somebody put there on purpose.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, out, err := opts.resolve()
			if err != nil {
				return err
			}
			raw, err := client.TaskParticipants(cmd.Context(), args[0])
			if err != nil {
				return fmt.Errorf("reading participants of %s: %w", args[0], err)
			}
			return renderParticipants(cmd.OutOrStdout(), out, raw)
		},
	}
}

// newCtlTasksAssignCmd puts somebody on a task.
func newCtlTasksAssignCmd(opts *ctlOptions) *cobra.Command {
	return &cobra.Command{
		Use:   "assign <uid> <user-uid>",
		Short: "Put somebody on a task — ask a particular person",
		Long: "Put somebody on a task.\n\n" +
			"Use it to ask a particular person before they have done anything about it.\n" +
			"`kukatko ctl people` lists the uids. Assigning somebody already on the task\n" +
			"is not an error; it only records that they were asked.",
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, out, err := opts.resolve()
			if err != nil {
				return err
			}
			raw, err := client.AssignTask(cmd.Context(), args[0], args[1])
			if err != nil {
				return fmt.Errorf("assigning %s to task %s: %w", args[1], args[0], err)
			}
			return renderParticipants(cmd.OutOrStdout(), out, raw)
		},
	}
}

// newCtlTasksUnassignCmd takes somebody off a task.
func newCtlTasksUnassignCmd(opts *ctlOptions) *cobra.Command {
	return &cobra.Command{
		Use:   "unassign <uid> <user-uid>",
		Short: "Take somebody off a task",
		Long: "Take somebody off a task.\n\n" +
			"Somebody who was not on it is not an error — the end state is the same\n" +
			"either way. What they did to the task stays in the audit trail.",
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, out, err := opts.resolve()
			if err != nil {
				return err
			}
			raw, err := client.UnassignTask(cmd.Context(), args[0], args[1])
			if err != nil {
				return fmt.Errorf("unassigning %s from task %s: %w", args[1], args[0], err)
			}
			return renderParticipants(cmd.OutOrStdout(), out, raw)
		},
	}
}

// newCtlPeopleCmd lists the library's accounts, which is where the uids the
// assign commands take come from.
func newCtlPeopleCmd(opts *ctlOptions) *cobra.Command {
	return &cobra.Command{
		Use:     "people",
		Aliases: []string{"users"},
		Short:   "List the library's accounts — the uids tasks are assigned to",
		Long: "List the accounts of the library: a uid and the name each is known by.\n\n" +
			"This is the directory every signed-in role may read. It is deliberately\n" +
			"not the administrative view: no addresses, no roles, no approval state\n" +
			"(those live under the admin API).",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			client, out, err := opts.resolve()
			if err != nil {
				return err
			}
			raw, err := client.People(cmd.Context())
			if err != nil {
				return fmt.Errorf("listing people: %w", err)
			}
			return renderDirectory(cmd.OutOrStdout(), out, raw)
		},
	}
}

// renderParticipants writes a task's people in the chosen format.
func renderParticipants(w io.Writer, out ctl.Output, raw json.RawMessage) error {
	return renderRaw(w, out, raw, "participants", ctl.DecodeParticipants, ctl.WriteParticipants)
}

// renderDirectory writes the library's accounts in the chosen format.
func renderDirectory(w io.Writer, out ctl.Output, raw json.RawMessage) error {
	return renderRaw(w, out, raw, "people", ctl.DecodeDirectory, ctl.WriteDirectory)
}
