package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/panbotka/kukatko/internal/database"
	"github.com/panbotka/kukatko/internal/importverify"
)

// errImportIncomplete is returned by the verify command (after printing the
// report) when the reconciliation found anything missing, so the process exits
// nonzero and a script or CI can gate on completeness.
var errImportIncomplete = errors.New("import is not complete: some items are missing")

// newImportVerifyCmd builds the "import verify" command: a read-only completeness
// check that reconciles the import sources (PhotoPrism photos/files, photo-sorter
// feeds vectors) against the Kukátko catalogue and reports whether the import is
// complete and nothing is missing. It exits nonzero when anything is missing.
func newImportVerifyCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "verify",
		Short: "Reconcile the import sources against the catalogue (completeness check)",
		Long: "Pull the source totals — PhotoPrism's photo and per-type counts and each photo's files, " +
			"and photo-sorter's feeds stats — and reconcile them against Kukátko, listing what is " +
			"missing: photos not imported, photos missing an original file (e.g. a RAW sibling), photos " +
			"missing their photo-sorter embedding/faces, and albums/labels/people not transferred. The " +
			"SHA256/SHA1-dedup delta is accounted for separately, so the remaining delta is a real gap. " +
			"Read-only: it records no import run. Exits nonzero when anything is missing.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runImportVerify(cmd)
		},
	}
	cmd.Flags().Bool("json", false, "print the full reconciliation report as JSON")
	return cmd
}

// runImportVerify loads the configuration, opens the database (applying
// migrations), builds the reconciler and runs it, printing the report (human
// summary or JSON). It returns errImportIncomplete when the report is not
// complete, so the command exits nonzero.
func runImportVerify(cmd *cobra.Command) error {
	cfg, err := loadConfigFromFlags(cmd)
	if err != nil {
		return err
	}
	asJSON, err := cmd.Flags().GetBool("json")
	if err != nil {
		return fmt.Errorf("reading --json: %w", err)
	}

	ctx := cmd.Context()
	db, err := database.New(ctx, cfg.Database)
	if err != nil {
		return fmt.Errorf("connecting to database: %w", err)
	}
	defer db.Close()
	if _, err := db.Migrate(ctx); err != nil {
		return fmt.Errorf("applying migrations: %w", err)
	}

	svc, err := buildImportVerifierService(cfg, db)
	if err != nil {
		return err
	}
	report, err := svc.Verify(ctx)
	if err != nil {
		return fmt.Errorf("verifying import completeness: %w", err)
	}

	if asJSON {
		if err := printReportJSON(cmd, report); err != nil {
			return err
		}
	} else {
		printReportSummary(cmd, report)
	}
	if !report.Complete {
		return errImportIncomplete
	}
	return nil
}

// printReportJSON prints the report as indented JSON.
func printReportJSON(cmd *cobra.Command, report importverify.Report) error {
	encoded, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return fmt.Errorf("encoding report: %w", err)
	}
	cmd.Println(string(encoded))
	return nil
}

// printReportSummary prints a human-readable reconciliation summary.
func printReportSummary(cmd *cobra.Command, report importverify.Report) {
	pp := report.PhotoPrism
	cmd.Printf("PhotoPrism photos: source=%d kukatko=%d deduplicated=%d missing=%d\n",
		pp.SourceTotal, pp.ImportedCount, pp.DeduplicatedCount, pp.MissingCount)
	if len(pp.SourceByType) > 0 {
		cmd.Printf("  by type: %s\n", formatByType(pp.SourceByType))
	}
	if len(pp.MissingUIDs) > 0 {
		cmd.Printf("  missing uids: %s\n", strings.Join(pp.MissingUIDs, ", "))
	}
	cmd.Printf("files missing (e.g. RAW sibling): %d\n", pp.FileGapCount)
	for _, gap := range pp.FileGaps {
		cmd.Printf("  %s expected=%d actual=%d\n", gap.PhotoprismUID, gap.Expected, gap.Actual)
	}
	printVectorsSummary(cmd, report.Vectors)
	printEntitySummary(cmd, "albums", report.Structure.Albums)
	printEntitySummary(cmd, "labels", report.Structure.Labels)
	printEntitySummary(cmd, "people", report.Structure.Subjects)
	if report.Complete {
		cmd.Println("=> COMPLETE: the import is complete, nothing is missing")
	} else {
		cmd.Println("=> INCOMPLETE: some items are missing (see above)")
	}
}

// printVectorsSummary prints the photo-sorter vectors reconciliation, or a note
// when the feeds source is not configured.
func printVectorsSummary(cmd *cobra.Command, v importverify.VectorsReport) {
	if v.NotConfigured {
		cmd.Println("vectors: photo-sorter feeds not configured (skipped)")
		return
	}
	cmd.Printf("vectors: embeddings source=%d kukatko=%d missing=%d; faces source=%d kukatko=%d missing=%d\n",
		v.SourcePhotosWithEmbeddings, v.CatalogEmbeddings, v.MissingEmbeddingsCount,
		v.SourceTotalFaces, v.CatalogFaces, v.MissingFacesCount)
	if len(v.MissingEmbeddings) > 0 {
		cmd.Printf("  photos missing embedding: %s\n", strings.Join(v.MissingEmbeddings, ", "))
	}
}

// printEntitySummary prints a structure entity's source-vs-catalogue counts and
// any missing names.
func printEntitySummary(cmd *cobra.Command, kind string, e importverify.EntityReport) {
	cmd.Printf("%s: source=%d kukatko=%d missing=%d\n", kind, e.SourceCount, e.CatalogCount, e.MissingCount)
	if len(e.Missing) > 0 {
		cmd.Printf("  missing: %s\n", strings.Join(e.Missing, ", "))
	}
}

// formatByType renders the per-type counts map as a stable "type=count" string.
func formatByType(byType map[string]int) string {
	parts := make([]string, 0, len(byType))
	for _, typ := range []string{"image", "raw", "video", "live", "animated", "vector", "audio"} {
		if n, ok := byType[typ]; ok {
			parts = append(parts, fmt.Sprintf("%s=%d", typ, n))
		}
	}
	return strings.Join(parts, " ")
}
