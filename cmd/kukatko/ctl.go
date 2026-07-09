package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/panbotka/kukatko/internal/ctl"
)

// ctlProgramName is the symlink name under which the ctl level is implied, so
// `kukatkoctl photos list` behaves exactly like `kukatko ctl photos list`.
const ctlProgramName = "kukatkoctl"

// Errors raised while configuring a ctl context.
var (
	// errTokenFlagConflict indicates --token and --token-stdin were both given.
	errTokenFlagConflict = errors.New("use either --token or --token-stdin, not both")
	// errEmptyTokenStdin indicates --token-stdin was given but stdin was blank.
	errEmptyTokenStdin = errors.New("no token read from stdin")
)

// impliesCtl reports whether the program was invoked through the kukatkoctl
// symlink, which drops the ctl level from every command line. argv0 is the raw
// os.Args[0]; only its base name matters.
func impliesCtl(argv0 string) bool {
	base := strings.TrimSuffix(filepath.Base(argv0), ".exe")
	return base == ctlProgramName
}

// ctlOptions holds the flags shared by every command in the ctl tree.
type ctlOptions struct {
	// contextName selects a context by name; empty means the current context.
	contextName string
	// output is the -o format, "table" or "json".
	output string
	// configPath overrides the client-side context file, mainly for tests.
	configPath string
}

// path returns the client-side context file to read and write.
func (o *ctlOptions) path() (string, error) {
	if o.configPath != "" {
		return o.configPath, nil
	}
	path, err := ctl.DefaultConfigPath()
	if err != nil {
		return "", fmt.Errorf("locating the ctl config file: %w", err)
	}
	return path, nil
}

// load reads the client-side context file, returning it together with its path.
func (o *ctlOptions) load() (*ctl.Config, string, error) {
	path, err := o.path()
	if err != nil {
		return nil, "", err
	}
	cfg, err := ctl.Load(path)
	if err != nil {
		return nil, "", fmt.Errorf("loading the ctl config: %w", err)
	}
	return cfg, path, nil
}

// resolve builds the API client for the selected context (with the KUKATKO_SERVER
// and KUKATKO_TOKEN overrides applied) and parses the requested output format.
func (o *ctlOptions) resolve() (*ctl.Client, ctl.Format, error) {
	format, err := ctl.ParseFormat(o.output)
	if err != nil {
		return nil, "", fmt.Errorf("reading --output: %w", err)
	}
	cfg, _, err := o.load()
	if err != nil {
		return nil, "", err
	}
	endpoint, err := ctl.Resolve(cfg, o.contextName, ctl.EnvFromOS())
	if err != nil {
		return nil, "", fmt.Errorf("selecting the server: %w", err)
	}
	client, err := ctl.NewClient(endpoint.Server, endpoint.Token)
	if err != nil {
		return nil, "", fmt.Errorf("building the API client: %w", err)
	}
	return client, format, nil
}

// newCtlCmd builds the "ctl" subcommand tree: a remote client that drives a
// running Kukátko instance over its HTTP API, rather than the database and
// filesystem every other subcommand acts on directly.
func newCtlCmd() *cobra.Command {
	opts := &ctlOptions{}
	cmd := &cobra.Command{
		Use:   "ctl",
		Short: "Drive a running Kukátko server over its HTTP API",
		Long: "ctl talks to a running Kukátko instance over /api/v1, authenticating with an\n" +
			"API token. Servers and tokens live in named contexts in " +
			"~/.config/kukatko/ctl.yaml;\n" + ctl.EnvServer + " and " + ctl.EnvToken +
			" override the active context.\n\n" +
			"Invoked through a symlink named " + ctlProgramName + ", the ctl level is implied.",
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	flags := cmd.PersistentFlags()
	flags.StringVar(&opts.contextName, "context", "",
		"name of the context to use (default: the current context)")
	flags.StringVarP(&opts.output, "output", "o", string(ctl.FormatTable),
		"output format: table or json")
	flags.StringVar(&opts.configPath, "ctl-config", "",
		"path to the client context file (default: ~/.config/kukatko/ctl.yaml)")

	cmd.AddCommand(newCtlConfigCmd(opts), newCtlPhotosCmd(opts))
	return cmd
}

// newCtlConfigCmd builds the "ctl config" tree, which manages the client-side
// contexts. Tokens are stored in a 0600 file and never printed back.
func newCtlConfigCmd(opts *ctlOptions) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "config",
		Short: "Manage client contexts (server URL and API token)",
	}
	cmd.AddCommand(newCtlConfigSetContextCmd(opts), newCtlConfigListCmd(opts),
		newCtlConfigUseContextCmd(opts))
	return cmd
}

// newCtlConfigSetContextCmd builds "ctl config set-context", which creates or
// updates a named context. The first context created becomes the current one.
func newCtlConfigSetContextCmd(opts *ctlOptions) *cobra.Command {
	var (
		server     string
		token      string
		tokenStdin bool
		current    bool
	)
	cmd := &cobra.Command{
		Use:   "set-context <name>",
		Short: "Create or update a context",
		Long: "Create or update a named context. The token is written to a file that is\n" +
			"always mode 0600 and is never printed back.\n\n" +
			"Prefer --token-stdin: a token passed with --token is visible to every other\n" +
			"process on the machine through the process list.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			resolved, err := readTokenFlag(cmd.InOrStdin(), token, tokenStdin)
			if err != nil {
				return err
			}
			return saveContext(cmd, opts, args[0], server, resolved, current)
		},
	}
	flags := cmd.Flags()
	flags.StringVar(&server, "server", "", "server root URL, e.g. https://kukatko.example.com")
	flags.StringVar(&token, "token", "", "API token (visible in the process list; prefer --token-stdin)")
	flags.BoolVar(&tokenStdin, "token-stdin", false, "read the API token from standard input")
	flags.BoolVar(&current, "current", false, "also make this context the current one")
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

// saveContext upserts the named context and persists the file. The very first
// context becomes current automatically, so a fresh install needs no extra step.
func saveContext(cmd *cobra.Command, opts *ctlOptions, name, server, token string, current bool) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return ctl.ErrContextNameRequired
	}
	cfg, path, err := opts.load()
	if err != nil {
		return err
	}
	entry, err := mergeContext(cfg, name, server, token)
	if err != nil {
		return err
	}
	cfg.Set(entry)
	if current || len(cfg.Contexts) == 1 {
		cfg.CurrentContext = name
	}
	if err := ctl.Save(path, cfg); err != nil {
		return fmt.Errorf("saving the ctl config: %w", err)
	}
	cmd.Printf("context %q saved to %s\n", name, path)
	if cfg.CurrentContext == name {
		cmd.Printf("current context is now %q\n", name)
	}
	return nil
}

// mergeContext folds the supplied server and token onto the existing context of
// that name, keeping every field the caller left blank — so updating a URL does
// not silently drop the stored token. A brand-new context needs a server URL.
func mergeContext(cfg *ctl.Config, name, server, token string) (ctl.Context, error) {
	entry, existed := cfg.Find(name)
	entry.Name = name
	if server != "" {
		entry.Server = server
	}
	if token != "" {
		entry.Token = token
	}
	if !existed && entry.Server == "" {
		return ctl.Context{}, ctl.ErrServerRequired
	}
	return entry, nil
}

// newCtlConfigListCmd builds "ctl config list", which prints the configured
// contexts. It always prints a table: a JSON dump would leak the tokens.
func newCtlConfigListCmd(opts *ctlOptions) *cobra.Command {
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

// newCtlConfigUseContextCmd builds "ctl config use-context", which switches the
// current context.
func newCtlConfigUseContextCmd(opts *ctlOptions) *cobra.Command {
	return &cobra.Command{
		Use:     "use-context <name>",
		Aliases: []string{"use"},
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

// renderPhotoPage writes a photo list or search result in the requested format:
// the API's own JSON bytes, unchanged, or a compact table.
func renderPhotoPage(w io.Writer, format ctl.Format, raw json.RawMessage) error {
	if format == ctl.FormatJSON {
		return writeRawJSON(w, raw)
	}
	page, err := ctl.DecodePhotoPage(raw)
	if err != nil {
		return fmt.Errorf("rendering the photo list: %w", err)
	}
	if err := ctl.WritePhotoPage(w, page); err != nil {
		return fmt.Errorf("rendering the photo list: %w", err)
	}
	return nil
}

// renderPhotoDetail writes a single photo in the requested format.
func renderPhotoDetail(w io.Writer, format ctl.Format, raw json.RawMessage) error {
	if format == ctl.FormatJSON {
		return writeRawJSON(w, raw)
	}
	detail, err := ctl.DecodePhotoDetail(raw)
	if err != nil {
		return fmt.Errorf("rendering the photo: %w", err)
	}
	if err := ctl.WritePhotoDetail(w, detail); err != nil {
		return fmt.Errorf("rendering the photo: %w", err)
	}
	return nil
}

// writeRawJSON echoes the API's response bytes for -o json.
func writeRawJSON(w io.Writer, raw json.RawMessage) error {
	if err := ctl.WriteJSON(w, raw); err != nil {
		return fmt.Errorf("writing json output: %w", err)
	}
	return nil
}
