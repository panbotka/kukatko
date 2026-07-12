package dupmerge

import "sort"

// plan is the resolved set of changes a merge will apply to the keeper: the
// album/label/subject associations it lacks that a copy carries, the scalar
// fields to fill, and the copies to archive. It is computed once (from the
// database) and then either applied (Merge) or reported as-is (Preview).
type plan struct {
	// albumsToAdd, labelsToAdd and subjectsToAdd are the association UIDs a copy
	// carries that the keeper does not, sorted for a deterministic apply order.
	albumsToAdd   []string
	labelsToAdd   []string
	subjectsToAdd []string
	// fill carries the gap-filling scalar values written to the keeper.
	fill scalarFill
	// archiveUIDs are the redundant copies that are still active and will be
	// archived (already-archived copies are omitted so a re-run is a no-op).
	archiveUIDs []string
}

// scalarFill carries the gap-filling scalar values a merge will write to the
// keeper. A nil pointer (or a false favorite) means "leave the keeper's value
// untouched"; a keeper value is only ever filled when it is empty.
type scalarFill struct {
	title       *string
	description *string
	favorite    bool
	rating      *int
	flag        *string
}

// filledFields returns the names of the scalar fields this fill sets, in a
// stable order, for the preview counts and the audit entry.
func (f scalarFill) filledFields() []string {
	fields := []string{}
	if f.title != nil {
		fields = append(fields, "title")
	}
	if f.description != nil {
		fields = append(fields, "description")
	}
	if f.rating != nil {
		fields = append(fields, "rating")
	}
	if f.favorite {
		fields = append(fields, "favorite")
	}
	if f.flag != nil {
		fields = append(fields, "flag")
	}
	return fields
}

// isEmpty reports whether the fill sets no scalar field at all.
func (f scalarFill) isEmpty() bool {
	return len(f.filledFields()) == 0
}

// isEmpty reports whether the plan would change nothing: no associations to add,
// no scalar fields to fill and no copies to archive. An empty plan is a no-op, so
// a re-run on an already-resolved group writes nothing.
func (p plan) isEmpty() bool {
	return len(p.albumsToAdd) == 0 && len(p.labelsToAdd) == 0 &&
		len(p.subjectsToAdd) == 0 && len(p.archiveUIDs) == 0 && p.fill.isEmpty()
}

// result projects the plan into the API-facing Result for the given keeper,
// tagging it as a dry run when reported by Preview.
func (p plan) result(keeperUID string, dryRun bool) Result {
	return Result{
		KeeperUID:      keeperUID,
		AlbumsAdded:    len(p.albumsToAdd),
		LabelsAdded:    len(p.labelsToAdd),
		PeopleAdded:    len(p.subjectsToAdd),
		MetadataFilled: p.fill.filledFields(),
		Archived:       len(p.archiveUIDs),
		DryRun:         dryRun,
	}
}

// photoRow holds the scalar fields of a group member needed to plan the merge:
// the columns that may be carried onto the keeper and whether the row is already
// archived (so an archived copy is not archived again).
type photoRow struct {
	uid         string
	title       string
	description string
	archived    bool
}

// subtract returns the sorted, de-duplicated elements of have that are not in
// remove — the associations a copy carries (have) that the keeper lacks
// (remove). The result is sorted so the apply order and audit details are stable.
func subtract(have, remove []string) []string {
	skip := make(map[string]bool, len(remove))
	for _, v := range remove {
		skip[v] = true
	}
	seen := make(map[string]bool, len(have))
	out := []string{}
	for _, v := range have {
		if skip[v] || seen[v] {
			continue
		}
		seen[v] = true
		out = append(out, v)
	}
	sort.Strings(out)
	return out
}

// pickFill returns a pointer to the first non-empty candidate when keeperValue is
// empty, or nil when the keeper already has a value (never overwrite) or no
// candidate offers one. It implements the "fill gaps only" rule for the string
// scalar fields.
func pickFill(keeperValue string, candidates []string) *string {
	if keeperValue != "" {
		return nil
	}
	for _, c := range candidates {
		if c != "" {
			value := c
			return &value
		}
	}
	return nil
}
