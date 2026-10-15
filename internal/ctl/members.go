package ctl

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

var (
	// ErrMemberNotFound indicates no member of the project has the username (or
	// uid) a command named.
	ErrMemberNotFound = errors.New("ctl: no such member of the project")
	// ErrNoIdentifier indicates an add that names no account.
	ErrNoIdentifier = errors.New("ctl: a username or e-mail address is required")
)

// Member is one member of a project as GET /p/{project}/members serves it to
// the project's admin (internal/memberapi).
type Member struct {
	UserUID     string     `json:"user_uid"`
	Username    string     `json:"username"`
	DisplayName string     `json:"display_name"`
	Role        string     `json:"role"`
	ApprovedAt  *time.Time `json:"approved_at"`
	LastSeenAt  *time.Time `json:"last_seen_at"`
	HasPicture  bool       `json:"has_picture"`
}

// membersPath renders the path of the member routes, plus an optional member
// uid. The client's project prefix names the project.
func membersPath(userUID string) string {
	path := "/members"
	if userUID != "" {
		path += "/" + url.PathEscape(userUID)
	}
	return path
}

// requireProject returns ErrNoProject for a client without a project: the
// member routes exist only under a project prefix.
func (c *Client) requireProject() error {
	if c.project == "" {
		return ErrNoProject
	}
	return nil
}

// ListMembers fetches GET /p/{project}/members: the project's members, as raw JSON
// (a bare array). Decode it with DecodeMembers.
func (c *Client) ListMembers(ctx context.Context) (json.RawMessage, error) {
	if err := c.requireProject(); err != nil {
		return nil, err
	}
	return c.get(ctx, membersPath(""), nil)
}

// AddMember asks the client's project to add the account identifier names — its
// exact username or e-mail address — with role, approved at once. The server
// answers the same whether the account was added, already was a member or
// does not exist, so neither does this: the raw answer is {"status":"accepted"}.
func (c *Client) AddMember(ctx context.Context, identifier, role string) (json.RawMessage, error) {
	if err := c.requireProject(); err != nil {
		return nil, err
	}
	if strings.TrimSpace(identifier) == "" {
		return nil, ErrNoIdentifier
	}
	body := map[string]string{"identifier": identifier, "role": role}
	return c.send(ctx, http.MethodPost, membersPath(""), body)
}

// WithdrawMember asks the client's project to undo an exact-match add: to
// remove the membership of the account identifier names while the account has
// not entered the project yet (its identity still withheld, so the member list
// does not show it). The server answers the same whatever it found, and so
// does this: the raw answer is {"status":"accepted"}.
func (c *Client) WithdrawMember(ctx context.Context, identifier string) (json.RawMessage, error) {
	if err := c.requireProject(); err != nil {
		return nil, err
	}
	if strings.TrimSpace(identifier) == "" {
		return nil, ErrNoIdentifier
	}
	return c.send(ctx, http.MethodPost, membersPath("")+"/withdraw", map[string]string{"identifier": identifier})
}

// SetMemberRole changes the role of the member userUID of the client's project and
// returns the member as raw JSON.
func (c *Client) SetMemberRole(ctx context.Context, userUID, role string) (json.RawMessage, error) {
	if err := c.requireProject(); err != nil {
		return nil, err
	}
	if err := requireUID("member", userUID); err != nil {
		return nil, err
	}
	return c.send(ctx, http.MethodPatch, membersPath(userUID), map[string]string{"role": role})
}

// ApproveMember approves the waiting member userUID of the client's project and
// returns the member as raw JSON.
func (c *Client) ApproveMember(ctx context.Context, userUID string) (json.RawMessage, error) {
	if err := c.requireProject(); err != nil {
		return nil, err
	}
	if err := requireUID("member", userUID); err != nil {
		return nil, err
	}
	return c.send(ctx, http.MethodPost, membersPath(userUID)+"/approve", nil)
}

// RemoveMember removes the member userUID from the client's project; the account
// itself stays.
func (c *Client) RemoveMember(ctx context.Context, userUID string) error {
	if err := c.requireProject(); err != nil {
		return err
	}
	if err := requireUID("member", userUID); err != nil {
		return err
	}
	if _, err := c.send(ctx, http.MethodDelete, membersPath(userUID), nil); err != nil {
		return err
	}
	return nil
}

// ResolveMember finds the member a command names by its username (or its uid)
// in the project's member list. It returns ErrMemberNotFound when none matches.
func (c *Client) ResolveMember(ctx context.Context, ref string) (Member, error) {
	raw, err := c.ListMembers(ctx)
	if err != nil {
		return Member{}, err
	}
	list, err := DecodeMembers(raw)
	if err != nil {
		return Member{}, err
	}
	for _, m := range list {
		if m.Username == ref || m.UserUID == ref {
			return m, nil
		}
	}
	return Member{}, fmt.Errorf("%w: %q", ErrMemberNotFound, ref)
}

// DecodeMembers decodes the bare array of GET /p/{project}/members.
func DecodeMembers(raw json.RawMessage) ([]Member, error) {
	var list []Member
	if err := json.Unmarshal(raw, &list); err != nil {
		return nil, fmt.Errorf("decoding the member list: %w", err)
	}
	return list, nil
}

// DecodeMember decodes one member.
func DecodeMember(raw json.RawMessage) (Member, error) {
	var m Member
	if err := json.Unmarshal(raw, &m); err != nil {
		return Member{}, fmt.Errorf("decoding the member: %w", err)
	}
	return m, nil
}

// approvalLabel renders a member's approval state.
func approvalLabel(m Member) string {
	if m.ApprovedAt == nil {
		return "pending"
	}
	return "approved"
}

// WriteMembers renders the members as a table: who, which role, whether they
// are let in yet and when they last signed in.
func WriteMembers(w io.Writer, list []Member) error {
	if len(list) == 0 {
		return writeLine(w, "no members found")
	}
	rows := make([][]string, 0, len(list))
	for _, m := range list {
		rows = append(rows, []string{
			m.Username, elide(dash(m.DisplayName), nameWidth), m.Role, approvalLabel(m),
			formatTime(m.LastSeenAt), m.UserUID,
		})
	}
	return writeTable(w, []string{"USERNAME", "NAME", "ROLE", "STATE", "LAST SEEN", "UID"}, rows)
}

// WriteMember renders one member as an aligned key/value table.
func WriteMember(w io.Writer, m Member) error {
	return writeKeyValues(w, [][2]string{
		{"UID", m.UserUID},
		{"USERNAME", m.Username},
		{"NAME", dash(m.DisplayName)},
		{"ROLE", m.Role},
		{"STATE", approvalLabel(m)},
		{"APPROVED", formatTime(m.ApprovedAt)},
		{"LAST SEEN", formatTime(m.LastSeenAt)},
	})
}
