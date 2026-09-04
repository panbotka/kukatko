package cluster

import (
	"testing"
	"time"
)

// TestPageRequestClamp covers the page bounds: an unstated limit falls back to
// the default, an oversized one is capped, and a negative offset reads as the
// first page.
func TestPageRequestClamp(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		req        PageRequest
		wantLimit  int
		wantOffset int
	}{
		{name: "zero asks for the default page", req: PageRequest{}, wantLimit: DefaultPageSize},
		{
			name: "a negative limit asks for the default page",
			req:  PageRequest{Limit: -3}, wantLimit: DefaultPageSize,
		},
		{
			name: "an oversized limit is capped",
			req:  PageRequest{Limit: MaxPageSize + 500}, wantLimit: MaxPageSize,
		},
		{
			name: "a negative offset is the first page",
			req:  PageRequest{Limit: 10, Offset: -7}, wantLimit: 10, wantOffset: 0,
		},
		{
			name: "an accepted page is left alone",
			req:  PageRequest{Limit: 10, Offset: 30}, wantLimit: 10, wantOffset: 30,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			limit, offset := tt.req.clamp()
			if limit != tt.wantLimit || offset != tt.wantOffset {
				t.Errorf("clamp() = %d/%d, want %d/%d", limit, offset, tt.wantLimit, tt.wantOffset)
			}
		})
	}
}

// TestNextOffset covers where the reader is told the next page starts, including
// the case where clusters were named away under them and the total shrank.
func TestNextOffset(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name                      string
		offset, limit, got, total int
		want                      *int
	}{
		{name: "a full page in the middle", offset: 0, limit: 10, got: 10, total: 30, want: new(10)},
		{name: "a short page is the last one", offset: 20, limit: 10, got: 4, total: 24},
		{name: "a full page at the end", offset: 20, limit: 10, got: 10, total: 30},
		{name: "an empty page is the last one", offset: 40, limit: 10, got: 0, total: 12},
		{
			name: "the total shrank under the reader", offset: 0, limit: 10, got: 10, total: 5,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := nextOffset(tt.offset, tt.limit, tt.got, tt.total)
			switch {
			case tt.want == nil && got != nil:
				t.Errorf("nextOffset = %d, want nil (no further page)", *got)
			case tt.want != nil && got == nil:
				t.Errorf("nextOffset = nil, want %d", *tt.want)
			case tt.want != nil && *got != *tt.want:
				t.Errorf("nextOffset = %d, want %d", *got, *tt.want)
			}
		})
	}
}

// TestViewOf builds the listing view from the cached summary rather than from
// the faces, which is the whole point of caching it.
func TestViewOf(t *testing.T) {
	t.Parallel()

	created := time.Date(2026, time.September, 1, 12, 0, 0, 0, time.UTC)
	rep := ExampleFace{PhotoUID: "ph1", FaceIndex: 0, BBox: [4]float64{0.1, 0.1, 0.2, 0.2}, DetScore: 0.9}
	suggestion := &Suggestion{SubjectUID: "su1", SubjectName: "Alice", Distance: 0.2, Confidence: 0.8}
	view := viewOf(Cluster{
		UID: "fc1", Size: 4, CreatedAt: created,
		Summary: &Summary{Representative: rep, Examples: []ExampleFace{rep}, Suggestion: suggestion},
	})

	if view.UID != "fc1" || view.Size != 4 || !view.CreatedAt.Equal(created) {
		t.Errorf("view = %+v, want fc1 of four faces created %s", view, created)
	}
	if view.Representative != rep || len(view.Examples) != 1 {
		t.Errorf("view faces = %+v / %+v, want the summary's", view.Representative, view.Examples)
	}
	if view.Suggestion == nil || view.Suggestion.SubjectName != "Alice" {
		t.Errorf("suggestion = %+v, want Alice", view.Suggestion)
	}
}

// TestViewOf_noSummary yields a drawable-but-empty view for a cluster that has
// not been prepared yet: never a nil examples slice, which would serialise as
// JSON null and break the client.
func TestViewOf_noSummary(t *testing.T) {
	t.Parallel()

	view := viewOf(Cluster{UID: "fc2", Size: 3})
	if view.Examples == nil {
		t.Error("examples = nil, want an empty slice")
	}
	if view.Suggestion != nil {
		t.Errorf("suggestion = %+v, want none", view.Suggestion)
	}
}
