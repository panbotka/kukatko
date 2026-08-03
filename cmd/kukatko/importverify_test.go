package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/panbotka/kukatko/internal/importverify"
)

// captureVectors renders printVectorsSummary into a buffer and returns its text.
func captureVectors(t *testing.T, vectors importverify.VectorsReport) string {
	t.Helper()

	cmd := &cobra.Command{}
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	printVectorsSummary(cmd, vectors)
	return buf.String()
}

// TestPrintVectorsSummary_partialCatalogue is the human-readable half of the
// docs/READINESS_AUDIT.md §2.3 regression guard. The fixture is that report's
// production shape: a catalogue that is a strict subset of the source, so no
// imported photo lacks its vectors (the gap is legitimately 0) while the
// catalogue holds 50 of 20 092 embeddings. The summary a reviewer reads at the
// point of no return must not present that as a finished vector migration — the
// zero has to arrive scoped, next to the coverage that contradicts it.
func TestPrintVectorsSummary_partialCatalogue(t *testing.T) {
	t.Parallel()

	out := captureVectors(t, importverify.VectorsReport{
		SourcePhotosWithEmbeddings:         20092,
		SourceTotalFaces:                   15000,
		CatalogEmbeddings:                  50,
		CatalogFaces:                       30,
		EmbeddingsSourceCoverage:           0.0025,
		FacesSourceCoverage:                0.002,
		EmbeddingsMissingForImportedPhotos: 0,
		FacesMissingForImportedPhotos:      0,
	})

	for _, want := range []string{
		"embeddings source=20092 kukatko=50 coverage=0.2%",
		"faces      source=15000 kukatko=30 coverage=0.2%",
		"missing-for-imported-photos=0",
		"NOTE:",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("summary missing %q:\n%s", want, out)
		}
	}
	// The bare, unscoped wording is what read as "embeddings are done".
	if strings.Contains(out, "missing=0") {
		t.Errorf("summary still prints an unscoped \"missing=0\":\n%s", out)
	}
}

// TestPrintVectorsSummary_fullCoverage checks the counterpart: with the catalogue
// holding the whole source, the coverage reads 100% and the scoping note — which
// only earns its space when the coverage is short — is left out.
func TestPrintVectorsSummary_fullCoverage(t *testing.T) {
	t.Parallel()

	out := captureVectors(t, importverify.VectorsReport{
		SourcePhotosWithEmbeddings: 100,
		SourceTotalFaces:           40,
		CatalogEmbeddings:          100,
		CatalogFaces:               40,
		EmbeddingsSourceCoverage:   1,
		FacesSourceCoverage:        1,
	})

	if !strings.Contains(out, "coverage=100.0%") {
		t.Errorf("summary should report full coverage:\n%s", out)
	}
	if strings.Contains(out, "NOTE:") {
		t.Errorf("the scoping note should be omitted at full coverage:\n%s", out)
	}
}

// TestPrintVectorsSummary_notConfigured checks that an unconfigured feeds source
// prints the skip note and none of the vector counters.
func TestPrintVectorsSummary_notConfigured(t *testing.T) {
	t.Parallel()

	out := captureVectors(t, importverify.VectorsReport{NotConfigured: true})

	if !strings.Contains(out, "photo-sorter feeds not configured") {
		t.Errorf("summary should note the skipped section:\n%s", out)
	}
	if strings.Contains(out, "coverage=") {
		t.Errorf("an unconfigured section should print no coverage:\n%s", out)
	}
}

// TestPrintVectorsSummary_listsMissingUIDs checks that the capped sample of
// imported photos without an embedding is printed under the counters.
func TestPrintVectorsSummary_listsMissingUIDs(t *testing.T) {
	t.Parallel()

	out := captureVectors(t, importverify.VectorsReport{
		SourcePhotosWithEmbeddings:         10,
		CatalogEmbeddings:                  8,
		EmbeddingsSourceCoverage:           0.8,
		EmbeddingsMissingForImportedPhotos: 2,
		EmbeddingsMissingUIDs:              []string{"ppA", "ppB"},
	})

	if !strings.Contains(out, "imported photos missing an embedding: ppA, ppB") {
		t.Errorf("summary should list the missing uids:\n%s", out)
	}
}

// TestPrintReportSummary_verdict checks the closing verdict line for both
// outcomes, since that is the line a reviewer reads first.
func TestPrintReportSummary_verdict(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		complete bool
		want     string
	}{
		{name: "complete", complete: true, want: "=> COMPLETE"},
		{name: "incomplete", complete: false, want: "=> INCOMPLETE"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			cmd := &cobra.Command{}
			var buf bytes.Buffer
			cmd.SetOut(&buf)
			cmd.SetErr(&buf)
			printReportSummary(cmd, importverify.Report{
				Vectors:  importverify.VectorsReport{NotConfigured: true},
				Complete: tt.complete,
			})

			if !strings.Contains(buf.String(), tt.want) {
				t.Errorf("summary should contain %q:\n%s", tt.want, buf.String())
			}
		})
	}
}

// TestFormatCoverage renders the [0,1] ratio as a one-decimal percentage across
// the range, including the sliver the audit measured.
func TestFormatCoverage(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		ratio float64
		want  string
	}{
		{name: "nothing covered", ratio: 0, want: "0.0%"},
		{name: "the audit's sliver", ratio: 0.0025, want: "0.2%"},
		{name: "a third", ratio: 0.3333, want: "33.3%"},
		{name: "everything", ratio: 1, want: "100.0%"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := formatCoverage(tt.ratio); got != tt.want {
				t.Errorf("formatCoverage(%v) = %q, want %q", tt.ratio, got, tt.want)
			}
		})
	}
}

// capturePhotoPrism renders printReportSummary's photo section into a buffer and
// returns its text.
func capturePhotoPrism(t *testing.T, pp importverify.PhotoPrismReport) string {
	t.Helper()

	cmd := &cobra.Command{}
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	printReportSummary(cmd, importverify.Report{
		PhotoPrism: pp,
		Vectors:    importverify.VectorsReport{NotConfigured: true},
	})
	return buf.String()
}

// TestPrintReportSummary_listingShortfall checks the shortfall gets the loud line
// it needs. The production report read
// "source=20660 kukatko=20647 deduplicated=13 missing=0 => COMPLETE" while the
// source held 20 677 pictures: the missing count was not wrong so much as
// unfounded, because the 17 absentees were never listed to be counted. A reader
// has to be told the numbers describe a window.
func TestPrintReportSummary_listingShortfall(t *testing.T) {
	t.Parallel()

	out := capturePhotoPrism(t, importverify.PhotoPrismReport{
		SourceTotal:         20660,
		SourceReportedTotal: 20677,
		ListingShortfall:    17,
		ImportedCount:       20647,
		DeduplicatedCount:   13,
	})

	for _, want := range []string{"LISTING SHORTFALL", "20677", "20660", "17"} {
		if !strings.Contains(out, want) {
			t.Errorf("summary should mention %q:\n%s", want, out)
		}
	}
}

// TestPrintReportSummary_noShortfallStaysQuiet checks a healthy listing prints no
// shortfall line, so the loud one keeps meaning something.
func TestPrintReportSummary_noShortfallStaysQuiet(t *testing.T) {
	t.Parallel()

	out := capturePhotoPrism(t, importverify.PhotoPrismReport{
		SourceTotal: 20677, SourceReportedTotal: 20677, ImportedCount: 20677,
	})

	if strings.Contains(out, "LISTING SHORTFALL") {
		t.Errorf("a complete listing must not print a shortfall:\n%s", out)
	}
}

// TestPrintReportSummary_surplusUIDs checks a catalogue photo the source listing
// no longer returns is named — the trace an upstream deletion leaves, and the
// only place it becomes visible.
func TestPrintReportSummary_surplusUIDs(t *testing.T) {
	t.Parallel()

	out := capturePhotoPrism(t, importverify.PhotoPrismReport{
		SourceTotal: 2, SourceReportedTotal: 2, ImportedCount: 3,
		SurplusCount: 1, SurplusUIDs: []string{"pteek3u9kw8oxi7y"},
	})

	if !strings.Contains(out, "only in kukatko") || !strings.Contains(out, "pteek3u9kw8oxi7y") {
		t.Errorf("summary should name the surplus uid:\n%s", out)
	}
}
