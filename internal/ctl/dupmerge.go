package ctl

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
)

// Sentinel errors of a duplicate merge, checked before a request is spent on an
// operation that archives photos.
var (
	// ErrNoMergeMembers indicates a merge naming only the keeper, which resolves
	// nothing and which the API rejects with 400.
	ErrNoMergeMembers = errors.New("ctl: a merge needs the keeper and at least one other photo")
)

// MergeInput is one duplicate group resolved into one of its members. MemberUIDs
// is the whole group including the keeper — the shape the API takes — and is
// built by MergeGroup so a caller cannot leave the keeper out of its own group.
type MergeInput struct {
	KeeperUID  string   `json:"keeper_uid"`
	MemberUIDs []string `json:"member_uids"`
	DryRun     bool     `json:"dry_run"`
}

// DuplicateMerge is what resolving a group did, or — when DryRun is set — what
// it would have done. Archived is the count that makes this destructive: those
// photos are in the trash afterwards, on the retention clock like anything else
// there.
type DuplicateMerge struct {
	KeeperUID      string   `json:"keeper_uid"`
	AlbumsAdded    int      `json:"albums_added"`
	LabelsAdded    int      `json:"labels_added"`
	PeopleAdded    int      `json:"people_added"`
	MetadataFilled []string `json:"metadata_filled"`
	Archived       int      `json:"archived"`
	DryRun         bool     `json:"dry_run"`
}

// MergeGroup resolves a duplicate group by merging every other member into the
// keeper: the albums, labels and people the copies carried move onto it, its
// empty metadata fields are filled from them, and the copies are **archived** —
// which is what makes this the one duplicates operation that changes the
// library rather than recording an opinion about it.
//
// dryRun asks the server to preview the whole thing (POST /duplicates/merge with
// dry_run), which computes exactly the same result and writes nothing. It is the
// same round trip either way, so what a dry run shows is what a merge does, not
// a second implementation's guess at it.
//
// others are the group's other members; the keeper is added to the membership
// here, so the API's "the keeper must be in the group" rejection is unreachable
// from ctl. The set is de-duplicated, and a group of one is refused locally with
// ErrNoMergeMembers.
func (c *Client) MergeGroup(
	ctx context.Context, keeperUID string, others []string, dryRun bool,
) (json.RawMessage, error) {
	in, err := mergeInput(keeperUID, others, dryRun)
	if err != nil {
		return nil, err
	}
	return c.send(ctx, http.MethodPost, "/duplicates/merge", in)
}

// mergeInput validates a merge and renders its request body.
func mergeInput(keeperUID string, others []string, dryRun bool) (MergeInput, error) {
	if err := requireUID("photo", keeperUID); err != nil {
		return MergeInput{}, err
	}
	members, err := NormalizeUIDs(append([]string{keeperUID}, others...))
	if err != nil {
		return MergeInput{}, fmt.Errorf("reading the group members: %w", err)
	}
	if len(members) < 2 {
		return MergeInput{}, fmt.Errorf("%w: only %s was named", ErrNoMergeMembers, keeperUID)
	}
	return MergeInput{KeeperUID: strings.TrimSpace(keeperUID), MemberUIDs: members, DryRun: dryRun}, nil
}

// DecodeDuplicateMerge decodes the answer of POST /duplicates/merge.
func DecodeDuplicateMerge(raw json.RawMessage) (DuplicateMerge, error) {
	var result DuplicateMerge
	if err := json.Unmarshal(raw, &result); err != nil {
		return DuplicateMerge{}, fmt.Errorf("decoding the merge result: %w", err)
	}
	return result, nil
}

// WriteDuplicateMerge renders what a merge moved onto the keeper and how many
// copies it archived, saying in the first word whether any of it happened.
func WriteDuplicateMerge(w io.Writer, result DuplicateMerge) error {
	rows := [][2]string{
		{"KEEPER", dash(result.KeeperUID)},
		{"ARCHIVED", strconv.Itoa(result.Archived) + " copies"},
		{"ALBUMS ADDED", strconv.Itoa(result.AlbumsAdded)},
		{"LABELS ADDED", strconv.Itoa(result.LabelsAdded)},
		{"PEOPLE ADDED", strconv.Itoa(result.PeopleAdded)},
		{"METADATA FILLED", dash(strings.Join(result.MetadataFilled, ", "))},
	}
	if err := writeKeyValues(w, rows); err != nil {
		return err
	}
	if result.DryRun {
		return writeLine(w, "\ndry run: nothing was merged and nothing was archived")
	}
	return writeLine(w, "\nthe archived copies are in the trash, not deleted")
}
