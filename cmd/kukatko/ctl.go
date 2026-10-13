package main

import (
	"encoding/json"
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

// impliesCtl reports whether the program was invoked through the kukatkoctl
// symlink, which drops the ctl level from every command line. argv0 is the raw
// os.Args[0]; only its base name matters.
func impliesCtl(argv0 string) bool {
	base := strings.TrimSuffix(filepath.Base(argv0), ".exe")
	return base == ctlProgramName
}

// ctlScopeAnnotation marks a top-level ctl command whose whole subtree talks to
// the instance-wide routes and so ignores the project; ctlScopeInstance is its
// one value. Every command without it is project-scoped.
const (
	ctlScopeAnnotation = "kukatko/ctl-scope"
	ctlScopeInstance   = "instance"
)

// ctlOptions holds the flags shared by every command in the ctl tree.
type ctlOptions struct {
	// contextName selects a context by name; empty means the current context.
	contextName string
	// project is the --project slug; empty defers to KUKATKO_PROJECT and then
	// to the context's own project.
	project string
	// output is the -o format: "table", "json" or "llm".
	output string
	// fields is the --fields allowlist, a comma-separated list of the JSON keys
	// the llm format should keep.
	fields string
	// configPath overrides the client-side context file, mainly for tests.
	configPath string
	// instance is set, before the command runs, when the command belongs to an
	// instance-level subtree (see markInstance): its client then talks to the
	// bare /api/v1 and no project is required.
	instance bool
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

// selection is what the command line selected: --context and --project.
func (o *ctlOptions) selection() ctl.Selection {
	return ctl.Selection{Context: o.contextName, Project: o.project}
}

// endpoint resolves the server, token and project of this invocation from the
// context file, the environment and the flags.
func (o *ctlOptions) endpoint() (ctl.Endpoint, error) {
	cfg, _, err := o.load()
	if err != nil {
		return ctl.Endpoint{}, err
	}
	endpoint, err := ctl.Resolve(cfg, o.selection(), ctl.EnvFromOS())
	if err != nil {
		return ctl.Endpoint{}, fmt.Errorf("selecting the server: %w", err)
	}
	return endpoint, nil
}

// resolve builds the API client for the selected context (with the KUKATKO_*
// overrides and the flags applied) and reads the requested output mode.
//
// A project-scoped command gets a client bound to its project, so every path it
// sends is /api/v1/p/<slug>/…; without a project it fails here, before any
// request, with ctl.ErrNoProject exactly. An instance-level command gets a
// client of the bare /api/v1 whatever project is selected.
func (o *ctlOptions) resolve() (*ctl.Client, ctl.Output, error) {
	out, err := ctl.NewOutput(o.output, ctl.ParseFields(o.fields))
	if err != nil {
		return nil, ctl.Output{}, fmt.Errorf("reading --output: %w", err)
	}
	endpoint, err := o.endpoint()
	if err != nil {
		return nil, ctl.Output{}, err
	}
	client, err := ctl.NewClient(endpoint.Server, endpoint.Token)
	if err != nil {
		return nil, ctl.Output{}, fmt.Errorf("building the API client: %w", err)
	}
	if o.instance {
		return client, out, nil
	}
	if endpoint.Project == "" {
		return nil, ctl.Output{}, ctl.ErrNoProject
	}
	return client.WithProject(endpoint.Project), out, nil
}

// markInstance marks cmd's subtree as instance-level (see ctlScopeAnnotation)
// and returns it.
func markInstance(cmd *cobra.Command) *cobra.Command {
	if cmd.Annotations == nil {
		cmd.Annotations = map[string]string{}
	}
	cmd.Annotations[ctlScopeAnnotation] = ctlScopeInstance
	return cmd
}

// isInstanceCommand reports whether cmd or one of its ancestors is marked
// instance-level.
func isInstanceCommand(cmd *cobra.Command) bool {
	for c := cmd; c != nil; c = c.Parent() {
		if c.Annotations[ctlScopeAnnotation] == ctlScopeInstance {
			return true
		}
	}
	return false
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
			"API token. Servers, tokens and projects live in named contexts in " +
			"~/.config/kukatko/ctl.yaml;\n" + ctl.EnvServer + ", " + ctl.EnvToken + " and " + ctl.EnvProject +
			" override the active context.\n\n" +
			"Every catalogue command works in one project, addressed as /api/v1/p/<slug>/:\n" +
			"--project beats " + ctl.EnvProject + ", which beats the context's project\n" +
			"(`kukatko ctl projects use <slug>`). The context, projects and users commands\n" +
			"are instance-wide and ignore it. One token serves every project its owner\n" +
			"is a member of.\n\n" +
			"Invoked through a symlink named " + ctlProgramName + ", the ctl level is implied.",
		SilenceUsage:  true,
		SilenceErrors: true,
		PersistentPreRunE: func(cmd *cobra.Command, _ []string) error {
			opts.instance = isInstanceCommand(cmd)
			return nil
		},
	}
	flags := cmd.PersistentFlags()
	flags.StringVar(&opts.contextName, "context", "",
		"name of the context to use (default: the current context)")
	flags.StringVar(&opts.project, "project", "",
		"slug of the project to work in (default: $"+ctl.EnvProject+", then the context's project)")
	flags.StringVarP(&opts.output, "output", "o", string(ctl.FormatTable),
		"output format: table, json (the server's own bytes) or llm (compact JSON for an agent)")
	flags.StringVar(&opts.fields, "fields", "",
		"comma-separated JSON keys to keep; narrows -o llm to an allowlist")
	flags.StringVar(&opts.configPath, "ctl-config", "",
		"path to the client context file (default: ~/.config/kukatko/ctl.yaml)")

	cmd.AddCommand(
		markInstance(newCtlContextCmd(opts)), newCtlPhotosCmd(opts), newCtlAlbumsCmd(opts), newCtlLabelsCmd(opts),
		newCtlSubjectsCmd(opts), newCtlFamilyCmd(opts), newCtlFavoritesCmd(opts),
		newCtlRatingCmd(opts), newCtlBulkCmd(opts),
		newCtlFacesCmd(opts), newCtlClustersCmd(opts), newCtlStacksCmd(opts),
		newCtlEditsCmd(opts), newCtlSavedSearchesCmd(opts), newCtlDuplicatesCmd(opts),
		newCtlCommentsCmd(opts), newCtlTasksCmd(opts), newCtlPeopleCmd(opts),
		newCtlTrashCmd(opts), newCtlUploadLinksCmd(opts),
		newCtlProcessCmd(opts), markInstance(newCtlProjectsCmd(opts)), markInstance(newCtlUsersCmd(opts)),
		newCtlMembersCmd(opts),
	)
	return cmd
}

// renderPhotoPage writes a photo list, a search result, a favorites page or a
// subject's gallery — all four share the /photos envelope — in the requested
// output mode: the API's own JSON bytes unchanged, a slimmed one, or a table.
func renderPhotoPage(w io.Writer, out ctl.Output, raw json.RawMessage) error {
	return renderRaw(w, out, raw, "photo list", ctl.DecodePhotoPage, ctl.WritePhotoPage)
}

// renderPhotoDetail writes a single photo in the requested format.
func renderPhotoDetail(w io.Writer, out ctl.Output, raw json.RawMessage) error {
	return renderRaw(w, out, raw, "photo", ctl.DecodePhotoDetail, ctl.WritePhotoDetail)
}

// writeRawJSON echoes the API's response bytes for -o json.
func writeRawJSON(w io.Writer, raw json.RawMessage) error {
	if err := ctl.WriteJSON(w, raw); err != nil {
		return fmt.Errorf("writing json output: %w", err)
	}
	return nil
}
