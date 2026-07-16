package mcpapi

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/panbotka/kukatko/internal/organize"
	"github.com/panbotka/kukatko/internal/people"
)

// listIn filters a collection listing by name. The lists are small enough to
// return whole, so this exists to save the agent context, not the database work.
//
//nolint:lll // jsonschema tags are unwrappable and are the agent-facing interface.
type listIn struct {
	Name string `json:"name,omitempty" jsonschema:"Keep only entries whose name contains this text (case-insensitive). Omit to list everything."`
}

// lookupIn identifies one album, label or subject by uid or slug.
type lookupIn struct {
	UID  string `json:"uid,omitempty" jsonschema:"The uid. Give either uid or slug."`
	Slug string `json:"slug,omitempty" jsonschema:"The slug (the url-safe short name). Give either uid or slug."`
}

// albumList, labelList and subjectList wrap the listings. Each is its own type so
// the tool's output schema names what it holds.
type albumList struct {
	Albums []albumInfo `json:"albums"`
}
type labelList struct {
	Labels []labelInfo `json:"labels"`
}
type subjectList struct {
	People []subjectInfo `json:"people"`
}

// errNeedUIDOrSlug is what a lookup tool answers when given neither key.
var errNeedUIDOrSlug = errors.New("give either a uid or a slug")

// registerCollectionTools adds the read tools for the three ways the library is
// organised: albums, labels and people.
func (a *API) registerCollectionTools(s *mcp.Server) {
	mcp.AddTool(s, &mcp.Tool{
		Name: "list_albums",
		Description: "List the albums with their photo counts. Use it to turn an album name a human " +
			"used into the uid the other tools want, or to see how the library is organised. " +
			"To read an album's photos, pass its uid to search_photos as album_uid.",
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true},
	}, a.handleListAlbums)

	mcp.AddTool(s, &mcp.Tool{
		Name:        "get_album",
		Description: "Read one album by uid or slug: its title, description, type and photo count.",
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true},
	}, a.handleGetAlbum)

	mcp.AddTool(s, &mcp.Tool{
		Name: "list_labels",
		Description: "List the labels with their photo counts. Labels are the library's own curated " +
			"tags (\"beach\", \"birthday\"), as opposed to albums, which are collections of specific " +
			"photos. To read a label's photos, pass its uid to search_photos as label_uid.",
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true},
	}, a.handleListLabels)

	mcp.AddTool(s, &mcp.Tool{
		Name:        "get_label",
		Description: "Read one label by uid or slug: its name and photo count.",
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true},
	}, a.handleGetLabel)

	mcp.AddTool(s, &mcp.Tool{
		Name: "list_subjects",
		Description: "List the people (and animals) the library knows by name, with how many " +
			"recognised faces each has. Use it to turn a name a human used into the uid the other " +
			"tools want. To read someone's photos, pass their uid to search_photos as person_uid.",
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true},
	}, a.handleListSubjects)

	mcp.AddTool(s, &mcp.Tool{
		Name:        "get_subject",
		Description: "Read one person or animal by uid or slug: their name, type, notes and face count.",
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true},
	}, a.handleGetSubject)
}

// handleListAlbums lists the albums, optionally filtered by name.
func (a *API) handleListAlbums(
	ctx context.Context, _ *mcp.CallToolRequest, in listIn,
) (*mcp.CallToolResult, albumList, error) {
	list, err := a.organize.ListAlbums(ctx)
	if err != nil {
		return nil, albumList{}, fmt.Errorf("listing albums: %w", err)
	}
	out := make([]albumInfo, 0, len(list))
	for _, al := range list {
		if !matchesName(al.Title, in.Name) {
			continue
		}
		out = append(out, toAlbumInfo(al.Album, al.PhotoCount))
	}
	return nil, albumList{Albums: out}, nil
}

// handleGetAlbum reads one album by uid or slug.
func (a *API) handleGetAlbum(
	ctx context.Context, _ *mcp.CallToolRequest, in lookupIn,
) (*mcp.CallToolResult, albumInfo, error) {
	al, err := a.lookupAlbum(ctx, in)
	if err != nil {
		return nil, albumInfo{}, err
	}
	uids, err := a.organize.ListPhotoUIDs(ctx, al.UID)
	if err != nil {
		return nil, albumInfo{}, fmt.Errorf("counting the album's photos: %w", err)
	}
	return nil, toAlbumInfo(al, len(uids)), nil
}

// lookupAlbum resolves an album from a uid or a slug.
func (a *API) lookupAlbum(ctx context.Context, in lookupIn) (organize.Album, error) {
	uid, slug := strings.TrimSpace(in.UID), strings.TrimSpace(in.Slug)
	var (
		al  organize.Album
		err error
	)
	switch {
	case uid != "":
		al, err = a.organize.GetAlbumByUID(ctx, uid)
	case slug != "":
		al, err = a.organize.GetAlbumBySlug(ctx, slug)
	default:
		return organize.Album{}, errNeedUIDOrSlug
	}
	if err != nil {
		if errors.Is(err, organize.ErrAlbumNotFound) {
			return organize.Album{}, fmt.Errorf("no album with %s", describeKey(uid, slug))
		}
		return organize.Album{}, fmt.Errorf("fetching album: %w", err)
	}
	return al, nil
}

// handleListLabels lists the labels, optionally filtered by name.
func (a *API) handleListLabels(
	ctx context.Context, _ *mcp.CallToolRequest, in listIn,
) (*mcp.CallToolResult, labelList, error) {
	list, err := a.organize.ListLabels(ctx)
	if err != nil {
		return nil, labelList{}, fmt.Errorf("listing labels: %w", err)
	}
	out := make([]labelInfo, 0, len(list))
	for _, l := range list {
		if !matchesName(l.Name, in.Name) {
			continue
		}
		out = append(out, toLabelInfo(l.Label, l.PhotoCount))
	}
	return nil, labelList{Labels: out}, nil
}

// handleGetLabel reads one label by uid or slug.
func (a *API) handleGetLabel(
	ctx context.Context, _ *mcp.CallToolRequest, in lookupIn,
) (*mcp.CallToolResult, labelInfo, error) {
	l, err := a.lookupLabel(ctx, in)
	if err != nil {
		return nil, labelInfo{}, err
	}
	uids, err := a.organize.ListPhotoUIDsByLabel(ctx, l.UID)
	if err != nil {
		return nil, labelInfo{}, fmt.Errorf("counting the label's photos: %w", err)
	}
	return nil, toLabelInfo(l, len(uids)), nil
}

// lookupLabel resolves a label from a uid or a slug.
func (a *API) lookupLabel(ctx context.Context, in lookupIn) (organize.Label, error) {
	uid, slug := strings.TrimSpace(in.UID), strings.TrimSpace(in.Slug)
	var (
		l   organize.Label
		err error
	)
	switch {
	case uid != "":
		l, err = a.organize.GetLabelByUID(ctx, uid)
	case slug != "":
		l, err = a.organize.GetLabelBySlug(ctx, slug)
	default:
		return organize.Label{}, errNeedUIDOrSlug
	}
	if err != nil {
		if errors.Is(err, organize.ErrLabelNotFound) {
			return organize.Label{}, fmt.Errorf("no label with %s", describeKey(uid, slug))
		}
		return organize.Label{}, fmt.Errorf("fetching label: %w", err)
	}
	return l, nil
}

// handleListSubjects lists the people, optionally filtered by name.
func (a *API) handleListSubjects(
	ctx context.Context, _ *mcp.CallToolRequest, in listIn,
) (*mcp.CallToolResult, subjectList, error) {
	list, err := a.people.ListSubjects(ctx)
	if err != nil {
		return nil, subjectList{}, fmt.Errorf("listing people: %w", err)
	}
	out := make([]subjectInfo, 0, len(list))
	for _, s := range list {
		if !matchesName(s.Name, in.Name) {
			continue
		}
		info := toSubjectInfo(s.Subject)
		info.FaceCount = s.MarkerCount
		out = append(out, info)
	}
	return nil, subjectList{People: out}, nil
}

// handleGetSubject reads one person by uid or slug.
func (a *API) handleGetSubject(
	ctx context.Context, _ *mcp.CallToolRequest, in lookupIn,
) (*mcp.CallToolResult, subjectInfo, error) {
	uid, slug := strings.TrimSpace(in.UID), strings.TrimSpace(in.Slug)
	var (
		subj people.Subject
		err  error
	)
	switch {
	case uid != "":
		subj, err = a.people.GetSubjectByUID(ctx, uid)
	case slug != "":
		subj, err = a.people.GetSubjectBySlug(ctx, slug)
	default:
		return nil, subjectInfo{}, errNeedUIDOrSlug
	}
	if err != nil {
		if errors.Is(err, people.ErrSubjectNotFound) {
			return nil, subjectInfo{}, fmt.Errorf("no person with %s", describeKey(uid, slug))
		}
		return nil, subjectInfo{}, fmt.Errorf("fetching person: %w", err)
	}
	uids, err := a.people.ListPhotoUIDsBySubject(ctx, subj.UID)
	if err != nil {
		return nil, subjectInfo{}, fmt.Errorf("counting the person's photos: %w", err)
	}
	info := toSubjectInfo(subj)
	info.PhotoCount = len(uids)
	return nil, info, nil
}

// matchesName reports whether name contains the (case-insensitive) filter, with
// an empty filter matching everything.
func matchesName(name, filter string) bool {
	if filter = strings.TrimSpace(filter); filter == "" {
		return true
	}
	return strings.Contains(strings.ToLower(name), strings.ToLower(filter))
}

// describeKey names whichever key a lookup was given, so a not-found error
// repeats back what the agent actually asked for.
func describeKey(uid, slug string) string {
	if uid != "" {
		return fmt.Sprintf("uid %q", uid)
	}
	return fmt.Sprintf("slug %q", slug)
}
