package ctl

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"testing"
)

// bulkResultBody is a realistic POST /photos/bulk response: a per-photo breakdown
// plus the aggregate counts. It shares its shape with no other endpoint.
const bulkResultBody = `{"results":[
	{"photo_uid":"pht01","status":"updated"},
	{"photo_uid":"pht02","status":"updated"},
	{"photo_uid":"pht03","status":"error","error":"photo not found"}
],"counts":{"total":3,"updated":2,"skipped":0,"errored":1}}`

// TestClient_Bulk_singleRequest is the load-bearing test of the bulk command: the
// whole batch travels in one POST, because the server applies it in one
// transaction. A per-photo loop would trade that atomicity for N transactions.
func TestClient_Bulk_singleRequest(t *testing.T) {
	t.Parallel()

	uids := make([]string, 0, 120)
	for i := range 120 {
		uids = append(uids, "pht"+strconv.Itoa(i))
	}

	var requests int
	var gotMethod, gotPath string
	var gotBody bulkRequest
	client := testClient(t, "kkt_a_b", func(w http.ResponseWriter, r *http.Request) {
		requests++
		gotMethod, gotPath = r.Method, r.URL.Path
		json.NewDecoder(r.Body).Decode(&gotBody)
		w.Write([]byte(bulkResultBody))
	})

	raw, err := client.Bulk(t.Context(), uids, BulkOperations{AddLabels: []string{"lbl01"}})
	if err != nil {
		t.Fatalf("Bulk returned %v", err)
	}
	if requests != 1 {
		t.Errorf("the client made %d requests, want exactly one for the whole batch", requests)
	}
	if gotMethod != http.MethodPost || gotPath != "/api/v1/photos/bulk" {
		t.Errorf("request = %s %s, want POST /api/v1/photos/bulk", gotMethod, gotPath)
	}
	if len(gotBody.PhotoUIDs) != 120 {
		t.Errorf("body carried %d uids, want all 120 in one request", len(gotBody.PhotoUIDs))
	}
	if len(gotBody.Operations.AddLabels) != 1 || gotBody.Operations.AddLabels[0] != "lbl01" {
		t.Errorf("operations = %+v, want the add-label operation", gotBody.Operations)
	}
	result, err := DecodeBulkResult(raw)
	if err != nil {
		t.Fatalf("DecodeBulkResult returned %v", err)
	}
	if result.Counts.Total != 3 || result.Counts.Errored != 1 || len(result.Results) != 3 {
		t.Errorf("result = %+v, want the decoded per-photo breakdown", result)
	}
}

// TestClient_Bulk_minimalBody verifies only the operations the caller asked for
// reach the wire. The endpoint rejects unknown fields and reads a bare false or a
// zero rating as a real change, so an omitted operation must really be omitted.
func TestClient_Bulk_minimalBody(t *testing.T) {
	t.Parallel()

	var gotBody map[string]any
	client := testClient(t, "kkt_a_b", func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&gotBody)
		w.Write([]byte(bulkResultBody))
	})

	if _, err := client.Bulk(t.Context(), []string{"pht01"}, BulkOperations{Archive: true}); err != nil {
		t.Fatalf("Bulk returned %v", err)
	}
	ops, ok := gotBody["operations"].(map[string]any)
	if !ok {
		t.Fatalf("body = %v, want an operations object", gotBody)
	}
	if ops["archive"] != true {
		t.Errorf("operations = %v, want archive true", ops)
	}
	for _, key := range []string{"set_rating", "set_favorite", "set_private", "unarchive", "clear_caption"} {
		if _, set := ops[key]; set {
			t.Errorf("operations = %v, want %q omitted rather than sent at its zero value", ops, key)
		}
	}
}

// TestClient_Bulk_dedupesUIDs verifies a repeated uid is sent once, so the batch
// the server transacts over matches the one the operator was asked to confirm.
func TestClient_Bulk_dedupesUIDs(t *testing.T) {
	t.Parallel()

	var gotBody bulkRequest
	client := testClient(t, "kkt_a_b", func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&gotBody)
		w.Write([]byte(bulkResultBody))
	})

	uids := []string{"pht01", " pht02 ", "pht01", ""}
	if _, err := client.Bulk(t.Context(), uids, BulkOperations{Archive: true}); err != nil {
		t.Fatalf("Bulk returned %v", err)
	}
	want := []string{"pht01", "pht02"}
	if len(gotBody.PhotoUIDs) != 2 || gotBody.PhotoUIDs[0] != want[0] || gotBody.PhotoUIDs[1] != want[1] {
		t.Errorf("photo_uids = %v, want %v", gotBody.PhotoUIDs, want)
	}
}

// TestClient_Bulk_invalid verifies an empty batch and an empty operation set never
// reach the network, where they would only earn a 400 and a rolled-back
// transaction.
func TestClient_Bulk_invalid(t *testing.T) {
	t.Parallel()

	client := testClient(t, "kkt_a_b", func(_ http.ResponseWriter, _ *http.Request) {
		t.Error("the server was contacted despite invalid input")
	})
	if _, err := client.Bulk(t.Context(), nil, BulkOperations{Archive: true}); !errors.Is(err, ErrNoPhotoUIDs) {
		t.Errorf("empty batch error = %v, want ErrNoPhotoUIDs", err)
	}
	if _, err := client.Bulk(t.Context(), []string{"pht01"}, BulkOperations{}); !errors.Is(err, ErrNoOperations) {
		t.Errorf("empty operations error = %v, want ErrNoOperations", err)
	}
}

// TestBulkOperations_Validate verifies the client mirrors the API's own rules, so
// a contradictory or out-of-range edit costs no round trip and no transaction.
func TestBulkOperations_Validate(t *testing.T) {
	t.Parallel()

	caption, description := "Lake", "Summer"
	tests := []struct {
		name    string
		ops     BulkOperations
		wantErr error
	}{
		{name: "empty", ops: BulkOperations{}, wantErr: ErrNoOperations},
		{
			name:    "caption set and cleared",
			ops:     BulkOperations{SetCaption: &caption, ClearCaption: true},
			wantErr: ErrConflictingOperations,
		},
		{
			name:    "description set and cleared",
			ops:     BulkOperations{SetDescription: &description, ClearDescription: true},
			wantErr: ErrConflictingOperations,
		},
		{
			name:    "location set and cleared",
			ops:     BulkOperations{SetLocation: &BulkLocation{}, ClearLocation: true},
			wantErr: ErrConflictingOperations,
		},
		{
			name:    "archive and unarchive",
			ops:     BulkOperations{Archive: true, Unarchive: true},
			wantErr: ErrConflictingOperations,
		},
		{name: "rating too high", ops: BulkOperations{SetRating: new(6)}, wantErr: ErrInvalidRating},
		{name: "rating negative", ops: BulkOperations{SetRating: new(-1)}, wantErr: ErrInvalidRating},
		{name: "unknown flag", ops: BulkOperations{SetFlag: new("maybe")}, wantErr: ErrInvalidFlag},
		{
			name:    "latitude out of range",
			ops:     BulkOperations{SetLocation: &BulkLocation{Lat: 91, Lng: 0}},
			wantErr: ErrInvalidLocation,
		},
		{name: "valid", ops: BulkOperations{SetRating: new(0), SetFlag: new(FlagNone)}, wantErr: nil},
		{name: "clear is not a conflict", ops: BulkOperations{ClearCaption: true}, wantErr: nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if err := tt.ops.Validate(); !errors.Is(err, tt.wantErr) {
				t.Errorf("Validate() error = %v, want %v", err, tt.wantErr)
			}
		})
	}
}

// TestParseLocation verifies a "lat,lng" pair parses and that anything else is
// refused rather than sent as a silent zero coordinate.
func TestParseLocation(t *testing.T) {
	t.Parallel()

	loc, err := ParseLocation(" 50.0755 , 14.4378 ")
	if err != nil {
		t.Fatalf("ParseLocation returned %v", err)
	}
	if loc.Lat != 50.0755 || loc.Lng != 14.4378 {
		t.Errorf("location = %+v, want the Prague coordinates", loc)
	}
	for _, in := range []string{"", "50.0755", "north,east", "50.0755,", "91,0", "0,181", "-91,0"} {
		if _, err := ParseLocation(in); !errors.Is(err, ErrInvalidLocation) {
			t.Errorf("ParseLocation(%q) error = %v, want ErrInvalidLocation", in, err)
		}
	}
}

// TestParsePhotoUIDs verifies every shape a caller might pipe in: the API's own
// list envelope, a bare array of uids, a bare array of photo objects, and a plain
// newline-separated list.
func TestParsePhotoUIDs(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   string
		want []string
	}{
		{
			name: "photos envelope from ctl photos list -o json",
			in:   `{"photos":[{"uid":"pht01","title":"a"},{"uid":"pht02"}],"total":2,"limit":100,"offset":0}`,
			want: []string{"pht01", "pht02"},
		},
		{name: "bare uid array", in: `["pht01","pht02"]`, want: []string{"pht01", "pht02"}},
		{name: "bare object array", in: `[{"uid":"pht01"},{"uid":"pht02"}]`, want: []string{"pht01", "pht02"}},
		{name: "newline separated", in: "pht01\npht02\n", want: []string{"pht01", "pht02"}},
		{name: "whitespace separated", in: "  pht01   pht02  ", want: []string{"pht01", "pht02"}},
		{name: "duplicates collapse", in: "pht01\npht02\npht01\n", want: []string{"pht01", "pht02"}},
		{name: "empty photo list", in: `{"photos":[],"total":0}`, want: nil},
		{name: "blank", in: "   \n ", want: nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := ParsePhotoUIDs(strings.NewReader(tt.in))
			if tt.want == nil {
				if !errors.Is(err, ErrNoPhotoUIDs) {
					t.Fatalf("ParsePhotoUIDs(%q) error = %v, want ErrNoPhotoUIDs", tt.in, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParsePhotoUIDs(%q) returned %v", tt.in, err)
			}
			if strings.Join(got, ",") != strings.Join(tt.want, ",") {
				t.Errorf("ParsePhotoUIDs(%q) = %v, want %v", tt.in, got, tt.want)
			}
		})
	}
}

// TestParsePhotoUIDs_malformed verifies broken JSON is reported rather than
// silently read as a whitespace-separated list of garbage.
func TestParsePhotoUIDs_malformed(t *testing.T) {
	t.Parallel()

	for _, in := range []string{`{"photos":`, `[1,2,`, `{"photos":[{"uid":1}]}`} {
		if _, err := ParsePhotoUIDs(strings.NewReader(in)); err == nil {
			t.Errorf("ParsePhotoUIDs(%q) returned no error", in)
		}
	}
}

// TestDecodeBulkResult_invalid verifies malformed JSON surfaces as an error.
func TestDecodeBulkResult_invalid(t *testing.T) {
	t.Parallel()

	if _, err := DecodeBulkResult([]byte(`{"counts":`)); err == nil {
		t.Error("DecodeBulkResult of malformed JSON returned no error")
	}
}
