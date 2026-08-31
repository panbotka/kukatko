package main

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"

	"github.com/panbotka/kukatko/internal/ctl"
)

// Errors raised by the confirmation gate that gates a large batch.
var (
	// errBatchNotConfirmed indicates the operator declined at the prompt.
	errBatchNotConfirmed = errors.New("aborted: the batch was not confirmed")
	// errConfirmationUnavailable indicates a large batch whose uid list arrived on
	// stdin, leaving nothing to read an answer from.
	errConfirmationUnavailable = errors.New("aborted: the batch needs --yes")
	// errNotConfirmed indicates an irreversible command run without --yes.
	errNotConfirmed = errors.New("aborted: this cannot be undone")
)

// photoUIDsFromArgs resolves the photo-uid set of a batch command from its
// positional arguments, or — when there are none — from stdin, so the command
// composes with `ctl photos list -o json`. It reports which of the two the uids
// came from, because a uid list read from stdin has consumed the very stream the
// confirmation prompt would read its answer from.
//
// Either way the set is trimmed, de-duplicated and required to be non-empty.
func photoUIDsFromArgs(cmd *cobra.Command, args []string) ([]string, bool, error) {
	if len(args) > 0 {
		uids, err := ctl.NormalizeUIDs(args)
		if err != nil {
			return nil, false, fmt.Errorf("reading the photo uids: %w", err)
		}
		return uids, false, nil
	}
	uids, err := ctl.ParsePhotoUIDs(cmd.InOrStdin())
	if err != nil {
		return nil, true, fmt.Errorf("reading the photo uids from stdin: %w", err)
	}
	return uids, true, nil
}

// confirmBatch gates a mutation that would touch more than ctl.ConfirmThreshold
// photos: it prints what is about to happen and waits for a yes. assumeYes (the
// --yes flag) skips the gate outright.
//
// When the uid list itself came from stdin the prompt cannot work — that stream is
// already drained, and a piped command has no terminal to answer from — so the
// gate fails with an error naming --yes rather than silently proceeding on an
// unanswerable question.
//
// action is the whole phrase, count included, that completes "About to …": for
// example "add 51 photos to album alb1a2b3".
func confirmBatch(cmd *cobra.Command, count int, assumeYes, uidsFromStdin bool, action string) error {
	if assumeYes || count <= ctl.ConfirmThreshold {
		return nil
	}
	if uidsFromStdin {
		return fmt.Errorf("%w: this would %s, more than the %d-photo threshold, and the uid list came "+
			"from stdin, so there is no answer to read; pass --yes to proceed",
			errConfirmationUnavailable, action, ctl.ConfirmThreshold)
	}
	cmd.Printf("About to %s, more than the %d-photo threshold. Continue? [y/N] ",
		action, ctl.ConfirmThreshold)
	answer, err := bufio.NewReader(cmd.InOrStdin()).ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return fmt.Errorf("reading the confirmation: %w", err)
	}
	switch strings.ToLower(strings.TrimSpace(answer)) {
	case "y", "yes":
		return nil
	default:
		return errBatchNotConfirmed
	}
}

// addConfirmFlag registers the --yes escape hatch of a batch command.
func addConfirmFlag(cmd *cobra.Command, assumeYes *bool) {
	cmd.Flags().BoolVarP(assumeYes, "yes", "y", false,
		fmt.Sprintf("do not ask for confirmation above %d photos", ctl.ConfirmThreshold))
}

// confirmIrreversible gates a command that destroys something the library cannot
// get back — merging two people into one, deleting a person. Unlike confirmBatch
// it has no threshold and asks no question: there is no size at which losing a
// person's name is harmless, and a ctl command is as likely to be run by an agent
// with no terminal as by a person with one. So the confirmation is the explicit
// --yes flag, and without it the command refuses, naming --dry-run as the way to
// see what it would have done.
//
// action is the whole phrase that completes "refusing to …".
func confirmIrreversible(assumeYes bool, action string) error {
	if assumeYes {
		return nil
	}
	return fmt.Errorf("%w: refusing to %s without --yes; --dry-run shows what it would do",
		errNotConfirmed, action)
}

// addIrreversibleFlags registers the two flags every irreversible ctl command
// carries: the confirmation and the rehearsal.
func addIrreversibleFlags(cmd *cobra.Command, assumeYes, dryRun *bool) {
	flags := cmd.Flags()
	flags.BoolVarP(assumeYes, "yes", "y", false, "confirm this irreversible change")
	flags.BoolVar(dryRun, "dry-run", false, "print what would happen and exit without writing")
}
