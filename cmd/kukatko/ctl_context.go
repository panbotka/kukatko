package main

import (
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"

	"github.com/panbotka/kukatko/internal/ctl"
	"github.com/panbotka/kukatko/internal/project"
)

// Errors raised while configuring a ctl context.
var (
	// errTokenFlagConflict indicates --token and --token-stdin were both given.
	errTokenFlagConflict = errors.New("use either --token or --token-stdin, not both")
	// errEmptyTokenStdin indicates --token-stdin was given but stdin was blank.
	errEmptyTokenStdin = errors.New("no token read from stdin")
)

// projectFlag is the name of the persistent --project flag, which "context set"
// reads rather than declaring a local flag of its own: a local --project would
// shadow the persistent one on that command.
const projectFlag = "project"

// newCtlContextCmd builds the "ctl context" tree (alias "config", its old name),
// which manages the client-side contexts. Tokens are stored in a 0600 file and
// never printed back. It is instance-level: no project is needed to run it.
func newCtlContextCmd(opts *ctlOptions) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "context",
		Aliases: []string{"config"},
		Short:   "Manage client contexts (server URL, API token and project)",
	}
	cmd.AddCommand(newCtlContextSetCmd(opts), newCtlContextListCmd(opts),
		newCtlContextUseCmd(opts), newCtlContextCurrentCmd(opts))
	return cmd
}

// contextEdit is one "context set": the fields the caller supplied. A nil
// project leaves the stored one alone; a pointer to "" clears it.
type contextEdit struct {
	server  string
	token   string
	project *string
	current bool
}

// newCtlContextSetCmd builds "ctl context set" (alias "set-context"), which
// creates or updates a named context — the current one when no name is given.
// The first context created becomes the current one.
func newCtlContextSetCmd(opts *ctlOptions) *cobra.Command {
	var (
		edit       contextEdit
		tokenStdin bool
	)
	cmd := &cobra.Command{
		Use:     "set [name]",
		Aliases: []string{"set-context"},
		Short:   "Create or update a context (the current one when no name is given)",
		Long: "Create or update a named context, or the current one when no name is given.\n" +
			"The token is written to a file that is always mode 0600 and is never printed\n" +
			"back. --project <slug> stores the project the catalogue commands work in;\n" +
			"--project \"\" clears it.\n\n" +
			"Prefer --token-stdin: a token passed with --token is visible to every other\n" +
			"process on the machine through the process list.",
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			resolved, err := readTokenFlag(cmd.InOrStdin(), edit.token, tokenStdin)
			if err != nil {
				return err
			}
			edit.token = resolved
			if cmd.Flags().Changed(projectFlag) {
				slug := strings.TrimSpace(opts.project)
				edit.project = &slug
			}
			name := ""
			named := len(args) == 1
			if named {
				name = args[0]
			}
			return saveContext(cmd, opts, name, named, edit)
		},
	}
	flags := cmd.Flags()
	flags.StringVar(&edit.server, "server", "", "server root URL, e.g. https://kukatko.example.com")
	flags.StringVar(&edit.token, "token", "", "API token (visible in the process list; prefer --token-stdin)")
	flags.BoolVar(&tokenStdin, "token-stdin", false, "read the API token from standard input")
	flags.BoolVar(&edit.current, "current", false, "also make this context the current one")
	return cmd
}

// readTokenFlag resolves the token from either --token or --token-stdin, which
// are mutually exclusive. An empty result means the caller supplied no token and
// any previously stored one must be kept.
func readTokenFlag(stdin io.Reader, token string, fromStdin bool) (string, error) {
	if !fromStdin {
		return token, nil
	}
	if token != "" {
		return "", errTokenFlagConflict
	}
	data, err := io.ReadAll(stdin)
	if err != nil {
		return "", fmt.Errorf("reading token from stdin: %w", err)
	}
	read := strings.TrimSpace(string(data))
	if read == "" {
		return "", errEmptyTokenStdin
	}
	return read, nil
}

// saveContext upserts the context name — the current one when the caller named
// none — and persists the file. The very first context becomes current
// automatically, so a fresh install needs no extra step.
func saveContext(cmd *cobra.Command, opts *ctlOptions, name string, named bool, edit contextEdit) error {
	cfg, path, err := opts.load()
	if err != nil {
		return err
	}
	name, err = contextToSet(cfg, name, named)
	if err != nil {
		return err
	}
	entry, err := mergeContext(cfg, name, edit)
	if err != nil {
		return err
	}
	cfg.Set(entry)
	if edit.current || len(cfg.Contexts) == 1 {
		cfg.CurrentContext = name
	}
	if err := ctl.Save(path, cfg); err != nil {
		return fmt.Errorf("saving the ctl config: %w", err)
	}
	cmd.Printf("context %q saved to %s\n", name, path)
	if edit.project != nil {
		cmd.Printf("project of context %q is now %s\n", name, describeProject(entry.Project))
	}
	if cfg.CurrentContext == name {
		cmd.Printf("current context is now %q\n", name)
	}
	return nil
}

// contextToSet returns the name of the context "context set" edits: the one
// named (which must not be blank), else the current one.
func contextToSet(cfg *ctl.Config, name string, named bool) (string, error) {
	name = strings.TrimSpace(name)
	switch {
	case named && name == "":
		return "", ctl.ErrContextNameRequired
	case named:
		return name, nil
	case cfg.CurrentContext == "":
		return "", ctl.ErrNoCurrentContext
	default:
		return cfg.CurrentContext, nil
	}
}

// mergeContext folds the supplied fields onto the existing context of that
// name, keeping every field the caller left out — so updating a URL does not
// silently drop the stored token or project. A brand-new context needs a server
// URL, and a project must be a well-formed slug.
func mergeContext(cfg *ctl.Config, name string, edit contextEdit) (ctl.Context, error) {
	entry, existed := cfg.Find(name)
	entry.Name = name
	if edit.server != "" {
		entry.Server = edit.server
	}
	if edit.token != "" {
		entry.Token = edit.token
	}
	if edit.project != nil {
		if *edit.project != "" {
			if err := project.ValidateSlug(*edit.project); err != nil {
				return ctl.Context{}, fmt.Errorf("setting the project: %w", err)
			}
		}
		entry.Project = *edit.project
	}
	if !existed && entry.Server == "" {
		return ctl.Context{}, ctl.ErrServerRequired
	}
	return entry, nil
}

// describeProject renders a context's project for a confirmation line.
func describeProject(slug string) string {
	if slug == "" {
		return "unset"
	}
	return fmt.Sprintf("%q", slug)
}

// newCtlContextListCmd builds "ctl context list", which prints the configured
// contexts. It always prints a table: a JSON dump would leak the tokens.
func newCtlContextListCmd(opts *ctlOptions) *cobra.Command {
	return &cobra.Command{
		Use:     "list",
		Aliases: []string{"get-contexts"},
		Short:   "List the configured contexts (tokens are never printed)",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, _, err := opts.load()
			if err != nil {
				return err
			}
			if err := ctl.WriteContexts(cmd.OutOrStdout(), cfg); err != nil {
				return fmt.Errorf("rendering the contexts: %w", err)
			}
			return nil
		},
	}
}

// newCtlContextUseCmd builds "ctl context use" (alias "use-context"), which
// switches the current context.
func newCtlContextUseCmd(opts *ctlOptions) *cobra.Command {
	return &cobra.Command{
		Use:     "use <name>",
		Aliases: []string{"use-context"},
		Short:   "Switch the current context",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, path, err := opts.load()
			if err != nil {
				return err
			}
			if err := cfg.Use(args[0]); err != nil {
				return fmt.Errorf("switching context: %w", err)
			}
			if err := ctl.Save(path, cfg); err != nil {
				return fmt.Errorf("saving the ctl config: %w", err)
			}
			cmd.Printf("current context is now %q\n", args[0])
			return nil
		},
	}
}

// newCtlContextCurrentCmd builds "ctl context current", which prints what the
// next command would talk to: the context, its server, whether a token is set
// and the project — after --project and the KUKATKO_* overrides, so it answers
// "where would this go" rather than "what does the file say".
func newCtlContextCurrentCmd(opts *ctlOptions) *cobra.Command {
	return &cobra.Command{
		Use:   "current",
		Short: "Show the context, server and project the next command will use",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			endpoint, err := opts.endpoint()
			if err != nil {
				return err
			}
			if err := ctl.WriteEndpoint(cmd.OutOrStdout(), endpoint); err != nil {
				return fmt.Errorf("rendering the context: %w", err)
			}
			return nil
		},
	}
}
