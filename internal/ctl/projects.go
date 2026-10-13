package ctl

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

var (
	// ErrProjectNotFound indicates no project of the instance has the slug (or
	// uid) a command named.
	ErrProjectNotFound = errors.New("ctl: no such project")
	// ErrAccountNotFound indicates no account of the instance has the username
	// (or uid) a command named.
	ErrAccountNotFound = errors.New("ctl: no such account")
	// ErrEmptyProjectPatch indicates a project update that sets nothing, which
	// would be a round trip for no change.
	ErrEmptyProjectPatch = errors.New("ctl: nothing to change — pass at least one field to update")
	// ErrConflictingLimit indicates one size limit both set and cleared.
	ErrConflictingLimit = errors.New("ctl: a limit cannot be set and cleared at once")
)

// Project is one project as the system administrator's routes serve it
// (project.Overview): the row plus its member count and usage.
type Project struct {
	UID            string    `json:"uid"`
	Slug           string    `json:"slug"`
	Name           string    `json:"name"`
	Description    string    `json:"description"`
	Active         bool      `json:"active"`
	StoragePrefix  string    `json:"storage_prefix"`
	MaxFileBytes   *int64    `json:"max_file_bytes"`
	MaxTotalBytes  *int64    `json:"max_total_bytes"`
	MembersCount   int64     `json:"members_count"`
	PhotosCount    int64     `json:"photos_count"`
	OriginalsBytes int64     `json:"originals_bytes"`
	CreatedAt      time.Time `json:"created_at"`
}

// AccountMembership is one project an account belongs to.
type AccountMembership struct {
	ProjectSlug string `json:"project_slug"`
	Role        string `json:"role"`
	Approved    bool   `json:"approved"`
}

// Account is one account of the instance as GET /users serves it to a system
// administrator.
type Account struct {
	UID          string              `json:"uid"`
	Username     string              `json:"username"`
	DisplayName  string              `json:"display_name"`
	Email        string              `json:"email"`
	InstanceRole string              `json:"instance_role"`
	Disabled     bool                `json:"disabled"`
	Memberships  []AccountMembership `json:"memberships"`
}

// ProjectInput is the body of a new project. A nil limit is no limit.
type ProjectInput struct {
	Slug          string `json:"slug"`
	Name          string `json:"name"`
	Description   string `json:"description,omitempty"`
	MaxFileBytes  *int64 `json:"max_file_bytes,omitempty"`
	MaxTotalBytes *int64 `json:"max_total_bytes,omitempty"`
}

// ProjectPatch is a project edit: a nil field is left as it is, a Clear flag
// removes that limit. The slug is absent on purpose — it never changes.
type ProjectPatch struct {
	Name               *string
	Description        *string
	Active             *bool
	MaxFileBytes       *int64
	MaxTotalBytes      *int64
	ClearMaxFileBytes  bool
	ClearMaxTotalBytes bool
}

// body renders the patch as the PATCH body: only the keys it sets, a cleared
// limit as an explicit null. It returns ErrEmptyProjectPatch when nothing is
// set and ErrConflictingLimit for a limit both set and cleared.
func (p ProjectPatch) body() (map[string]any, error) {
	body := map[string]any{}
	if p.Name != nil {
		body["name"] = *p.Name
	}
	if p.Description != nil {
		body["description"] = *p.Description
	}
	if p.Active != nil {
		body["active"] = *p.Active
	}
	for _, limit := range []struct {
		key   string
		set   *int64
		clear bool
	}{
		{"max_file_bytes", p.MaxFileBytes, p.ClearMaxFileBytes},
		{"max_total_bytes", p.MaxTotalBytes, p.ClearMaxTotalBytes},
	} {
		switch {
		case limit.set != nil && limit.clear:
			return nil, fmt.Errorf("%w: %s", ErrConflictingLimit, limit.key)
		case limit.set != nil:
			body[limit.key] = *limit.set
		case limit.clear:
			body[limit.key] = nil
		}
	}
	if len(body) == 0 {
		return nil, ErrEmptyProjectPatch
	}
	return body, nil
}

// ListProjects fetches GET /projects: every project with its counts, as raw
// JSON (a bare array). Decode it with DecodeProjects.
func (c *Client) ListProjects(ctx context.Context) (json.RawMessage, error) {
	return c.get(ctx, "/projects", nil)
}

// GetProject fetches one project by uid as raw JSON.
func (c *Client) GetProject(ctx context.Context, uid string) (json.RawMessage, error) {
	if err := requireUID("project", uid); err != nil {
		return nil, err
	}
	return c.get(ctx, projectPath(uid), nil)
}

// CreateProject creates a project; the caller becomes its first admin.
func (c *Client) CreateProject(ctx context.Context, in ProjectInput) (json.RawMessage, error) {
	return c.send(ctx, http.MethodPost, "/projects", in)
}

// UpdateProject applies patch to the project uid and returns it as it now
// stands. An empty patch costs no round trip (ErrEmptyProjectPatch).
func (c *Client) UpdateProject(ctx context.Context, uid string, patch ProjectPatch) (json.RawMessage, error) {
	if err := requireUID("project", uid); err != nil {
		return nil, err
	}
	body, err := patch.body()
	if err != nil {
		return nil, err
	}
	return c.send(ctx, http.MethodPatch, projectPath(uid), body)
}

// AddProjectMember adds the account userUID to the project projectUID with
// role, approved at once, and returns the membership as raw JSON.
func (c *Client) AddProjectMember(ctx context.Context, projectUID, userUID, role string) (json.RawMessage, error) {
	if err := requireUID("project", projectUID); err != nil {
		return nil, err
	}
	if userUID == "" {
		return nil, ErrNoUser
	}
	body := map[string]string{"user_uid": userUID, "role": role}
	return c.send(ctx, http.MethodPost, projectPath(projectUID)+"/members", body)
}

// ListAccounts fetches GET /users: every account of the instance with its
// instance role and memberships, as raw JSON (a bare array).
func (c *Client) ListAccounts(ctx context.Context) (json.RawMessage, error) {
	return c.get(ctx, "/users", nil)
}

// SetInstanceRole promotes the account uid to a system administrator
// ("maintainer") or demotes it ("user"), returning the account as raw JSON.
func (c *Client) SetInstanceRole(ctx context.Context, uid, role string) (json.RawMessage, error) {
	if err := requireUID("account", uid); err != nil {
		return nil, err
	}
	return c.send(ctx, http.MethodPatch, "/users/"+url.PathEscape(uid), map[string]string{"instance_role": role})
}

// ResolveProject finds the project a command names by its slug (or its uid) in
// the instance's project list. It returns ErrProjectNotFound when none matches.
func (c *Client) ResolveProject(ctx context.Context, ref string) (Project, error) {
	raw, err := c.ListProjects(ctx)
	if err != nil {
		return Project{}, err
	}
	list, err := DecodeProjects(raw)
	if err != nil {
		return Project{}, err
	}
	for _, p := range list {
		if p.Slug == ref || p.UID == ref {
			return p, nil
		}
	}
	return Project{}, fmt.Errorf("%w: %q", ErrProjectNotFound, ref)
}

// ResolveAccount finds the account a command names by its username (or its
// uid) in the instance's account list. It returns ErrAccountNotFound when none
// matches.
func (c *Client) ResolveAccount(ctx context.Context, ref string) (Account, error) {
	raw, err := c.ListAccounts(ctx)
	if err != nil {
		return Account{}, err
	}
	list, err := DecodeAccounts(raw)
	if err != nil {
		return Account{}, err
	}
	for _, a := range list {
		if a.Username == ref || a.UID == ref {
			return a, nil
		}
	}
	return Account{}, fmt.Errorf("%w: %q", ErrAccountNotFound, ref)
}

// projectPath renders the path of one project's routes.
func projectPath(uid string) string {
	return "/projects/" + url.PathEscape(uid)
}

// DecodeProjects decodes the bare array of GET /projects.
func DecodeProjects(raw json.RawMessage) ([]Project, error) {
	var list []Project
	if err := json.Unmarshal(raw, &list); err != nil {
		return nil, fmt.Errorf("decoding the project list: %w", err)
	}
	return list, nil
}

// DecodeProject decodes one project.
func DecodeProject(raw json.RawMessage) (Project, error) {
	var p Project
	if err := json.Unmarshal(raw, &p); err != nil {
		return Project{}, fmt.Errorf("decoding the project: %w", err)
	}
	return p, nil
}

// DecodeAccounts decodes the bare array of GET /users.
func DecodeAccounts(raw json.RawMessage) ([]Account, error) {
	var list []Account
	if err := json.Unmarshal(raw, &list); err != nil {
		return nil, fmt.Errorf("decoding the account list: %w", err)
	}
	return list, nil
}

// DecodeAccount decodes one account.
func DecodeAccount(raw json.RawMessage) (Account, error) {
	var a Account
	if err := json.Unmarshal(raw, &a); err != nil {
		return Account{}, fmt.Errorf("decoding the account: %w", err)
	}
	return a, nil
}

// formatLimit renders a size limit, "unlimited" when there is none.
func formatLimit(limit *int64) string {
	if limit == nil {
		return "unlimited"
	}
	return formatSize(*limit)
}

// activeLabel renders a project's active flag as a word.
func activeLabel(active bool) string {
	if active {
		return "active"
	}
	return "inactive"
}

// WriteProjects renders the projects as a table: who is in each, how much it
// holds and what it may hold.
func WriteProjects(w io.Writer, list []Project) error {
	if len(list) == 0 {
		return writeLine(w, "no projects found")
	}
	rows := make([][]string, 0, len(list))
	for _, p := range list {
		rows = append(rows, []string{
			p.Slug, elide(p.Name, nameWidth), activeLabel(p.Active),
			strconv.FormatInt(p.MembersCount, 10), strconv.FormatInt(p.PhotosCount, 10),
			formatSize(p.OriginalsBytes), formatLimit(p.MaxFileBytes), formatLimit(p.MaxTotalBytes), p.UID,
		})
	}
	return writeTable(w, []string{"SLUG", "NAME", "STATE", "MEMBERS", "PHOTOS", "ORIGINALS", "MAX FILE",
		"QUOTA", "UID"}, rows)
}

// WriteProject renders one project as an aligned key/value table.
func WriteProject(w io.Writer, p Project) error {
	prefix := p.StoragePrefix
	if prefix == "" {
		prefix = "(storage root)"
	}
	return writeKeyValues(w, [][2]string{
		{"UID", p.UID},
		{"SLUG", p.Slug},
		{"NAME", p.Name},
		{"DESCRIPTION", dash(p.Description)},
		{"STATE", activeLabel(p.Active)},
		{"MEMBERS", strconv.FormatInt(p.MembersCount, 10)},
		{"PHOTOS", strconv.FormatInt(p.PhotosCount, 10)},
		{"ORIGINALS", formatSize(p.OriginalsBytes)},
		{"MAX FILE", formatLimit(p.MaxFileBytes)},
		{"QUOTA", formatLimit(p.MaxTotalBytes)},
		{"STORAGE PREFIX", prefix},
		{"CREATED", formatStamp(p.CreatedAt)},
	})
}

// formatMemberships renders an account's memberships as "slug (role)", a
// pending one marked, or a dash for none.
func formatMemberships(list []AccountMembership) string {
	if len(list) == 0 {
		return "-"
	}
	parts := make([]string, 0, len(list))
	for _, m := range list {
		part := m.ProjectSlug + " (" + m.Role
		if !m.Approved {
			part += ", pending"
		}
		parts = append(parts, part+")")
	}
	return strings.Join(parts, ", ")
}

// accountState renders an account's instance role and its disabled flag.
func accountState(a Account) string {
	if a.Disabled {
		return a.InstanceRole + ", disabled"
	}
	return a.InstanceRole
}

// WriteAccounts renders the accounts as a table with their instance role and
// memberships.
func WriteAccounts(w io.Writer, list []Account) error {
	if len(list) == 0 {
		return writeLine(w, "no accounts found")
	}
	rows := make([][]string, 0, len(list))
	for _, a := range list {
		rows = append(rows, []string{
			a.Username, elide(dash(a.DisplayName), nameWidth), accountState(a), formatMemberships(a.Memberships), a.UID,
		})
	}
	return writeTable(w, []string{"USERNAME", "NAME", "INSTANCE ROLE", "PROJECTS", "UID"}, rows)
}

// WriteAccount renders one account as an aligned key/value table.
func WriteAccount(w io.Writer, a Account) error {
	return writeKeyValues(w, [][2]string{
		{"UID", a.UID},
		{"USERNAME", a.Username},
		{"NAME", dash(a.DisplayName)},
		{"E-MAIL", dash(a.Email)},
		{"INSTANCE ROLE", accountState(a)},
		{"PROJECTS", formatMemberships(a.Memberships)},
	})
}
