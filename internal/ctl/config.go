// Package ctl implements the client side of `kukatko ctl`: a small HTTP client
// for Kukátko's own /api/v1, the kubectl-style context file that points it at a
// server, and the compact renderers the CLI prints.
//
// It has nothing to do with internal/config, which describes a *server* and has
// no notion of a remote endpoint. Nothing here touches the database, the job
// queue or the originals store; the only state this package owns is a single
// client-side file, ~/.config/kukatko/ctl.yaml, which holds bearer tokens and is
// therefore only ever written with mode 0600.
package ctl

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"go.yaml.in/yaml/v3"
)

// Environment variables that override the active context, one field at a time.
const (
	// EnvServer overrides the active context's server URL.
	EnvServer = "KUKATKO_SERVER"
	// EnvToken overrides the active context's API token.
	EnvToken = "KUKATKO_TOKEN" //nolint:gosec // The name of a variable, not a credential.
)

const (
	// configDirName is the per-application directory under the user config dir.
	configDirName = "kukatko"
	// configFileName is the client-side context file inside that directory.
	configFileName = "ctl.yaml"
	// configFileMode is the only mode the context file is ever written with: it
	// stores bearer tokens, so nobody but the owner may read it.
	configFileMode os.FileMode = 0o600
	// configDirMode keeps the containing directory owner-only as well.
	configDirMode os.FileMode = 0o700
)

// Sentinel errors for context resolution.
var (
	// ErrContextNotFound indicates the requested (or current) context name has no
	// entry in the context file.
	ErrContextNotFound = errors.New("ctl: context not found")
	// ErrNoServer indicates neither the environment nor any context supplied a
	// server URL, so there is nothing to talk to.
	ErrNoServer = errors.New(
		"ctl: no server configured; set " + EnvServer +
			" or run `kukatko ctl config set-context <name> --server <url>`")
	// ErrServerRequired indicates a new context was declared without a server URL.
	ErrServerRequired = errors.New("ctl: a new context needs --server")
	// ErrContextNameRequired indicates an empty context name.
	ErrContextNameRequired = errors.New("ctl: context name is required")
)

// Context names one Kukátko server and the API token used to reach it, in the
// style of a kubectl context. Server is the site root (for example
// https://kukatko.example.com), not the /api/v1 base path — the client appends
// that itself.
type Context struct {
	Name   string `yaml:"name"`
	Server string `yaml:"server"`
	Token  string `yaml:"token,omitempty"`
}

// Config is the whole client-side context file: a list of named contexts and the
// name of the one currently selected. A zero Config is a valid empty file.
type Config struct {
	CurrentContext string    `yaml:"current-context,omitempty"`
	Contexts       []Context `yaml:"contexts,omitempty"`
}

// Find returns the context with the given name, reporting whether it exists.
func (c *Config) Find(name string) (Context, bool) {
	for _, ctx := range c.Contexts {
		if ctx.Name == name {
			return ctx, true
		}
	}
	return Context{}, false
}

// Set inserts ctx, or replaces the existing context of the same name, storing
// the server URL in normalized form. It does not change which context is current.
func (c *Config) Set(ctx Context) {
	ctx.Server = NormalizeServer(ctx.Server)
	for i := range c.Contexts {
		if c.Contexts[i].Name == ctx.Name {
			c.Contexts[i] = ctx
			return
		}
	}
	c.Contexts = append(c.Contexts, ctx)
}

// Use marks the named context as current, returning ErrContextNotFound when no
// such context exists.
func (c *Config) Use(name string) error {
	if _, ok := c.Find(name); !ok {
		return fmt.Errorf("%w: %q", ErrContextNotFound, name)
	}
	c.CurrentContext = name
	return nil
}

// DefaultConfigPath returns the path of the client-side context file,
// ~/.config/kukatko/ctl.yaml on Linux (os.UserConfigDir honours XDG_CONFIG_HOME).
func DefaultConfigPath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("locating user config dir: %w", err)
	}
	return filepath.Join(dir, configDirName, configFileName), nil
}

// Load reads the context file at path. A missing file is not an error: it yields
// an empty Config, so a first run driven purely by KUKATKO_SERVER and
// KUKATKO_TOKEN works without any file at all.
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path) //nolint:gosec // path is the user's own config file.
	if errors.Is(err, os.ErrNotExist) {
		return &Config{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", path, err)
	}
	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parsing %s: %w", path, err)
	}
	return &cfg, nil
}

// Save writes cfg to path atomically, creating the directory as needed. The file
// carries API tokens, so it is created 0600 and chmodded back to 0600 after the
// rename — an existing world-readable file is tightened rather than reused.
func Save(path string, cfg *Config) error {
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("encoding ctl config: %w", err)
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, configDirMode); err != nil {
		return fmt.Errorf("creating %s: %w", dir, err)
	}
	tmp, err := os.CreateTemp(dir, configFileName+".*")
	if err != nil {
		return fmt.Errorf("creating temp file in %s: %w", dir, err)
	}
	if err := writeAndClose(tmp, data); err != nil {
		_ = os.Remove(tmp.Name())
		return err
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		_ = os.Remove(tmp.Name())
		return fmt.Errorf("replacing %s: %w", path, err)
	}
	if err := os.Chmod(path, configFileMode); err != nil {
		return fmt.Errorf("restricting %s to %o: %w", path, configFileMode, err)
	}
	return nil
}

// writeAndClose writes data to f with the mode the context file demands and
// closes it, so Save's caller can remove the temp file on any failure.
func writeAndClose(f *os.File, data []byte) error {
	if err := f.Chmod(configFileMode); err != nil {
		_ = f.Close()
		return fmt.Errorf("restricting %s to %o: %w", f.Name(), configFileMode, err)
	}
	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		return fmt.Errorf("writing %s: %w", f.Name(), err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("closing %s: %w", f.Name(), err)
	}
	return nil
}

// Env carries the two environment overrides. An empty field means "not set" and
// leaves the context's own value in place.
type Env struct {
	Server string
	Token  string
}

// EnvFromOS reads the KUKATKO_SERVER and KUKATKO_TOKEN overrides from the
// process environment.
func EnvFromOS() Env {
	return Env{Server: os.Getenv(EnvServer), Token: os.Getenv(EnvToken)}
}

// Endpoint is the server and credential one invocation resolved to.
// ContextName is empty when the endpoint came from the environment alone.
type Endpoint struct {
	ContextName string
	Server      string
	Token       string
}

// Resolve picks the endpoint for one invocation. contextName selects a context
// explicitly; when empty, the file's current context is used, and when that is
// empty too, no context contributes anything. The environment then overrides the
// server and the token independently, so KUKATKO_TOKEN alone can re-credential a
// stored context.
//
// It returns ErrContextNotFound when a named (or current) context is absent, and
// ErrNoServer when nothing supplied a server URL. The returned Server has any
// trailing slash trimmed.
func Resolve(cfg *Config, contextName string, env Env) (Endpoint, error) {
	if cfg == nil {
		cfg = &Config{}
	}
	name := contextName
	if name == "" {
		name = cfg.CurrentContext
	}
	var ep Endpoint
	if name != "" {
		ctx, ok := cfg.Find(name)
		if !ok {
			return Endpoint{}, fmt.Errorf("%w: %q", ErrContextNotFound, name)
		}
		ep = Endpoint{ContextName: ctx.Name, Server: ctx.Server, Token: ctx.Token}
	}
	if env.Server != "" {
		ep.Server = env.Server
	}
	if env.Token != "" {
		ep.Token = env.Token
	}
	if ep.Server == "" {
		return Endpoint{}, ErrNoServer
	}
	ep.Server = NormalizeServer(ep.Server)
	return ep, nil
}

// NormalizeServer trims surrounding space and trailing slashes from a server URL,
// so the client can concatenate the API base path onto it without doubling the
// separator and a stored context holds one canonical spelling.
func NormalizeServer(server string) string {
	return strings.TrimRight(strings.TrimSpace(server), "/")
}
