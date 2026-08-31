package main

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/panbotka/kukatko/internal/ctl"
)

// newCtlSavedSearchesCmd builds the "ctl saved-searches" tree: the named library
// views the app calls smart albums (`internal/savedsearchapi`).
//
// They are strictly **per-user**: the token scopes every operation, and one
// belonging to somebody else answers 404 — never 403, which would confirm it
// exists. The CLI reports that as "not yours", naming both readings, because the
// server deliberately refuses to say which it is. Any signed-in role may keep
// their own; there is no editor check, since a saved search curates nobody's
// view but the owner's.
func newCtlSavedSearchesCmd(opts *ctlOptions) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "saved-searches",
		Aliases: []string{"smart-albums"},
		Short:   "List, create, edit and delete your own saved searches (\"smart albums\")",
	}
	cmd.AddCommand(
		newCtlSavedSearchesListCmd(opts), newCtlSavedSearchesGetCmd(opts),
		newCtlSavedSearchesCreateCmd(opts), newCtlSavedSearchesUpdateCmd(opts),
		newCtlSavedSearchesDeleteCmd(opts),
	)
	return cmd
}

// newCtlSavedSearchesListCmd builds "ctl saved-searches list".
func newCtlSavedSearchesListCmd(opts *ctlOptions) *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List your own saved searches, newest first",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			client, out, err := opts.resolve()
			if err != nil {
				return err
			}
			raw, err := client.ListSavedSearches(cmd.Context())
			if err != nil {
				return fmt.Errorf("listing saved searches: %w", err)
			}
			return renderSavedSearches(cmd.OutOrStdout(), out, raw)
		},
	}
}

// newCtlSavedSearchesGetCmd builds "ctl saved-searches get <uid>".
func newCtlSavedSearchesGetCmd(opts *ctlOptions) *cobra.Command {
	return &cobra.Command{
		Use:   "get <uid>",
		Short: "Show one saved search and the view it stores",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, out, err := opts.resolve()
			if err != nil {
				return err
			}
			raw, err := client.GetSavedSearch(cmd.Context(), args[0])
			if err != nil {
				return fmt.Errorf("fetching saved search %s: %w", args[0], err)
			}
			return renderSavedSearch(cmd.OutOrStdout(), out, raw)
		},
	}
}

// searchParamsFlags registers the two ways of stating a saved search's stored
// view, which are mutually exclusive.
func searchParamsFlags(cmd *cobra.Command, params *string, pairs *[]string) {
	flags := cmd.Flags()
	flags.StringVar(params, "params", "",
		`the whole stored view as a JSON object of strings, e.g. '{"q":"jezero","mode":"semantic"}'`)
	flags.StringArrayVar(pairs, "param", nil,
		"one key=value of the stored view; repeatable (e.g. --param q=jezero --param year=2024)")
}

// resolveSearchParams folds --params and --param into the one view to store,
// returning nil when the caller wrote neither, which an update reads as "leave
// the stored view alone".
func resolveSearchParams(cmd *cobra.Command, params string, pairs []string) (map[string]string, error) {
	if !cmd.Flags().Changed("params") && len(pairs) == 0 {
		//nolint:nilnil // no view given: a nil map means "leave the stored view alone".
		return nil, nil
	}
	if cmd.Flags().Changed("params") && len(pairs) > 0 {
		return nil, fmt.Errorf("%w: use either --params or --param, not both", ctl.ErrInvalidSearchParams)
	}
	view, err := ctl.ParseSearchParams(params)
	if err != nil {
		return nil, fmt.Errorf("reading --params: %w", err)
	}
	for _, pair := range pairs {
		key, value, err := ctl.ParseSearchParam(pair)
		if err != nil {
			return nil, fmt.Errorf("reading --param: %w", err)
		}
		view[key] = value
	}
	return view, nil
}

// newCtlSavedSearchesCreateCmd builds "ctl saved-searches create <name>".
func newCtlSavedSearchesCreateCmd(opts *ctlOptions) *cobra.Command {
	var (
		params string
		pairs  []string
	)
	cmd := &cobra.Command{
		Use:   "create <name>",
		Short: "Save a named library view of your own",
		Long: "Save a named library view.\n\n" +
			"The stored view is the flat key/value object the app puts in its URL — the\n" +
			"filters, the sort, the query `q` and the search `mode` — so a search saved\n" +
			"here opens in the web UI exactly as it was written. Every value must be a\n" +
			"string, which is what the app reads them as.\n\n" +
			"It is yours alone: nobody else can list it, read it or open it.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, out, err := opts.resolve()
			if err != nil {
				return err
			}
			view, err := resolveSearchParams(cmd, params, pairs)
			if err != nil {
				return err
			}
			raw, err := client.CreateSavedSearch(cmd.Context(), args[0], view)
			if err != nil {
				return fmt.Errorf("creating saved search %q: %w", args[0], err)
			}
			return renderSavedSearch(cmd.OutOrStdout(), out, raw)
		},
	}
	searchParamsFlags(cmd, &params, &pairs)
	return cmd
}

// newCtlSavedSearchesUpdateCmd builds "ctl saved-searches update <uid>".
func newCtlSavedSearchesUpdateCmd(opts *ctlOptions) *cobra.Command {
	var (
		name   string
		params string
		pairs  []string
	)
	cmd := &cobra.Command{
		Use:   "update <uid>",
		Short: "Rename one of your saved searches or replace the view it stores",
		Long: "Rename a saved search or replace the view it stores.\n\n" +
			"Unlike an album or a label, this endpoint genuinely merges: an omitted field\n" +
			"is left alone server-side, so the command needs no read first. The stored\n" +
			"view, though, is replaced whole — --param adds to what you write here, not to\n" +
			"what is already stored.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, out, err := opts.resolve()
			if err != nil {
				return err
			}
			view, err := resolveSearchParams(cmd, params, pairs)
			if err != nil {
				return err
			}
			raw, err := client.UpdateSavedSearch(cmd.Context(), args[0],
				optionalString(cmd, "name", name), view)
			if err != nil {
				return fmt.Errorf("updating saved search %s: %w", args[0], err)
			}
			return renderSavedSearch(cmd.OutOrStdout(), out, raw)
		},
	}
	cmd.Flags().StringVar(&name, "name", "", "new name for the saved search")
	searchParamsFlags(cmd, &params, &pairs)
	return cmd
}

// newCtlSavedSearchesDeleteCmd builds "ctl saved-searches delete <uid>".
func newCtlSavedSearchesDeleteCmd(opts *ctlOptions) *cobra.Command {
	return &cobra.Command{
		Use:   "delete <uid>",
		Short: "Delete one of your saved searches",
		Long: "Delete one of your saved searches.\n\n" +
			"No photo is touched: a saved search is a stored question, not a collection.\n" +
			"It asks for no confirmation for exactly that reason — asking it again costs\n" +
			"one command.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, out, err := opts.resolve()
			if err != nil {
				return err
			}
			if err := client.DeleteSavedSearch(cmd.Context(), args[0]); err != nil {
				return fmt.Errorf("deleting saved search %s: %w", args[0], err)
			}
			return renderAck(cmd.OutOrStdout(), out,
				"saved search "+args[0]+" deleted; no photo was touched")
		},
	}
}
