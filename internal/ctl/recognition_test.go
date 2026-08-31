package ctl

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

// TestSubjectLabel verifies a person is reported name-first with the uid beside
// it, and that each half alone still reads.
func TestSubjectLabel(t *testing.T) {
	t.Parallel()

	cases := map[string]struct{ name, uid, want string }{
		"both":    {"Anna", "sub01", "Anna (sub01)"},
		"uid":     {"", "sub01", "sub01"},
		"name":    {"Anna", "", "Anna"},
		"neither": {"", "", "-"},
	}
	for label, tc := range cases {
		t.Run(label, func(t *testing.T) {
			t.Parallel()

			if got := SubjectLabel(tc.name, tc.uid); got != tc.want {
				t.Errorf("SubjectLabel(%q, %q) = %q, want %q", tc.name, tc.uid, got, tc.want)
			}
		})
	}
}

// TestWriteFaceList verifies the table names the people rather than only their
// uids, keeps the unnamed rows legible, and closes with a summary saying how much
// work the photo still holds.
func TestWriteFaceList(t *testing.T) {
	t.Parallel()

	list, err := DecodeFaceList(json.RawMessage(facesBody))
	if err != nil {
		t.Fatalf("DecodeFaceList returned %v", err)
	}
	var buf bytes.Buffer
	if err := WriteFaceList(&buf, list); err != nil {
		t.Fatalf("WriteFaceList returned %v", err)
	}
	out := buf.String()
	for _, want := range []string{
		"FACE", "WHO", "MARKER", "SUGGESTS",
		"Anna (sub01)", "mrk01", "Bob 0.310", "-1",
		"photo pht01 · 3 faces · 1 named · 2 unnamed",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("face table does not contain %q:\n%s", want, out)
		}
	}
}

// TestWriteFaceList_empty verifies a photo with no detections prints one line and
// no header, like every other empty listing.
func TestWriteFaceList_empty(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	if err := WriteFaceList(&buf, FaceList{PhotoUID: "pht09"}); err != nil {
		t.Fatalf("WriteFaceList returned %v", err)
	}
	if buf.String() != "no faces found on photo pht09\n" {
		t.Errorf("empty face table = %q, want a single line", buf.String())
	}
}

// TestWriteFaceAssign verifies an assignment says who the marker now names, and a
// detach says outright that it names nobody rather than printing an empty name.
func TestWriteFaceAssign(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	named := FaceAssignResult{
		Action:  FaceActionAssignPerson,
		Marker:  Marker{UID: "mrk01", PhotoUID: "pht01"},
		Subject: &Subject{UID: "sub02", Name: "Bob"},
	}
	if err := WriteFaceAssign(&buf, named); err != nil {
		t.Fatalf("WriteFaceAssign returned %v", err)
	}
	if !strings.Contains(buf.String(), "now names Bob (sub02)") {
		t.Errorf("assignment line = %q, want the person named", buf.String())
	}

	buf.Reset()
	detached := FaceAssignResult{
		Action: FaceActionUnassignPerson,
		Marker: Marker{UID: "mrk01", PhotoUID: "pht01"},
	}
	if err := WriteFaceAssign(&buf, detached); err != nil {
		t.Fatalf("WriteFaceAssign returned %v", err)
	}
	if !strings.Contains(buf.String(), "now names nobody") {
		t.Errorf("detach line = %q, want it to say nobody is named", buf.String())
	}
}

// TestWriteClusters verifies the cluster table carries the suggested identity with
// its distance, dashes the groups nobody was close enough for, and totals the
// unnamed faces waiting.
func TestWriteClusters(t *testing.T) {
	t.Parallel()

	clusters, err := DecodeClusters(json.RawMessage(clustersBody))
	if err != nil {
		t.Fatalf("DecodeClusters returned %v", err)
	}
	var buf bytes.Buffer
	if err := WriteClusters(&buf, clusters); err != nil {
		t.Fatalf("WriteClusters returned %v", err)
	}
	out := buf.String()
	for _, want := range []string{
		"UID", "FACES", "SUGGESTION", "REPRESENTATIVE",
		"Anna (sub01) 0.280", "pht01 #0", "2 clusters · 15 unnamed faces",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("cluster table does not contain %q:\n%s", want, out)
		}
	}
}

// TestWriteClusters_empty verifies nothing to name prints one line.
func TestWriteClusters_empty(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	if err := WriteClusters(&buf, nil); err != nil {
		t.Fatalf("WriteClusters returned %v", err)
	}
	if buf.String() != "no clusters found\n" {
		t.Errorf("empty cluster table = %q, want a single line", buf.String())
	}
}

// TestWriteClusterAssign verifies naming a whole cluster reports the person by
// name and how many markers that wrote.
func TestWriteClusterAssign(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	result := ClusterAssignResult{
		ClusterUID: "clu01",
		Subject:    Subject{UID: "sub07", Name: "Cyril"},
		Markers:    []Marker{{UID: "mrk01"}, {UID: "mrk02"}},
	}
	if err := WriteClusterAssign(&buf, result); err != nil {
		t.Fatalf("WriteClusterAssign returned %v", err)
	}
	want := "cluster clu01 assigned to Cyril (sub07): 2 markers written\n"
	if buf.String() != want {
		t.Errorf("cluster assignment = %q, want %q", buf.String(), want)
	}
}

// TestWriteClusterRemoval verifies both outcomes read differently: a smaller
// cluster, and one the removal emptied.
func TestWriteClusterRemoval(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	if err := WriteClusterRemoval(&buf, "clu01", &Cluster{UID: "clu01", Size: 1}); err != nil {
		t.Fatalf("WriteClusterRemoval returned %v", err)
	}
	if buf.String() != "cluster clu01 now holds 1 face\n" {
		t.Errorf("removal line = %q, want the smaller cluster", buf.String())
	}
	buf.Reset()
	if err := WriteClusterRemoval(&buf, "clu01", nil); err != nil {
		t.Fatalf("WriteClusterRemoval returned %v", err)
	}
	if !strings.Contains(buf.String(), "was removed") {
		t.Errorf("orphan line = %q, want it to say the cluster is gone", buf.String())
	}
}

// TestWriteMergeReport verifies the merge result names both people in every
// format — the one result ctl synthesizes rather than passing through, because the
// merge itself deletes the only other record of the source's name.
func TestWriteMergeReport(t *testing.T) {
	t.Parallel()

	report := MergeReport{
		MergeResult: MergeResult{
			KeeperUID: "sub02", SourceUID: "sub01", MarkersMoved: 17, SharedPhotos: 3,
		},
		KeeperName: "Anna Nováková",
		SourceName: "Anna N.",
	}

	var buf bytes.Buffer
	if err := WriteMergeReport(&buf, Output{Format: FormatTable}, report); err != nil {
		t.Fatalf("WriteMergeReport returned %v", err)
	}
	for _, want := range []string{"KEEPER", "Anna Nováková (sub02)", "MERGED AWAY", "Anna N. (sub01)", "17"} {
		if !strings.Contains(buf.String(), want) {
			t.Errorf("merge table does not contain %q:\n%s", want, buf.String())
		}
	}

	buf.Reset()
	if err := WriteMergeReport(&buf, Output{Format: FormatLLM}, report); err != nil {
		t.Fatalf("WriteMergeReport returned %v", err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(buf.Bytes(), &decoded); err != nil {
		t.Fatalf("llm merge report is not valid JSON: %v (%q)", err, buf.String())
	}
	if decoded["source_name"] != "Anna N." || decoded["keeper_uid"] != "sub02" {
		t.Errorf("llm merge report = %v, want both names flattened beside the counts", decoded)
	}
	if _, present := decoded["dismissals_moved"]; present {
		t.Errorf("llm merge report = %v, want the zero counts dropped", decoded)
	}
}
