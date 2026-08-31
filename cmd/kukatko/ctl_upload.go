package main

import (
	"errors"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/panbotka/kukatko/internal/ctl"
)

// errUploadFailed indicates that at least one file of an upload was not
// catalogued, so the command ends nonzero even though the request itself
// succeeded — the report is printed either way.
var errUploadFailed = errors.New("some files were not uploaded")

// newCtlPhotosUploadCmd builds "ctl photos upload <path…>", the way into the
// library from a terminal.
func newCtlPhotosUploadCmd(opts *ctlOptions) *cobra.Command {
	return &cobra.Command{
		Use:   "upload <path>…",
		Short: "Upload files into the library through the ordinary ingest path (editor or admin)",
		Long: "Upload one or more files into the library.\n\n" +
			"They go through the same ingest path as the web uploader: the server hashes\n" +
			"each file and refuses to store the same bytes twice, reads its metadata,\n" +
			"renders its thumbnails and queues the follow-up work (embeddings, faces, the\n" +
			"metadata sidecar). Nothing about that happens here — this command only carries\n" +
			"the bytes there.\n\n" +
			"The upload is streamed: a file is copied from disk into the request as it is\n" +
			"sent, so a hundred-megabyte original never sits in memory, and there is no\n" +
			"timeout on the transfer beyond the one you interrupt it with.\n\n" +
			"A file whose bytes are already in the library is reported as a duplicate, not\n" +
			"as an error: that is the deduplication working. Only a file that could not be\n" +
			"catalogued at all makes the command exit nonzero.\n\n" +
			"To ingest a whole directory tree, run `kukatko import dir` beside the library\n" +
			"instead — this command takes files, not directories.",
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runPhotoUpload(cmd, opts, args)
		},
	}
}

// runPhotoUpload streams the named files to the server and renders the per-file
// report, returning an error when any of them failed.
func runPhotoUpload(cmd *cobra.Command, opts *ctlOptions, paths []string) error {
	client, out, err := opts.resolve()
	if err != nil {
		return err
	}
	raw, err := client.UploadPhotos(cmd.Context(), paths)
	if err != nil {
		return fmt.Errorf("uploading %d files: %w", len(paths), err)
	}
	if err := renderUploadReport(cmd.OutOrStdout(), out, raw); err != nil {
		return err
	}
	return uploadOutcomeError(raw)
}

// uploadOutcomeError turns a report carrying failed files into the command's
// exit status. The report has already been printed, so the error only has to say
// how many files it names — a pipeline reads the status, a human reads the table.
func uploadOutcomeError(raw []byte) error {
	report, err := ctl.DecodeUploadReport(raw)
	if err != nil {
		return fmt.Errorf("reading the upload result: %w", err)
	}
	if counts := report.Counts(); counts.Failed > 0 {
		return fmt.Errorf("%w: %d of %d files", errUploadFailed, counts.Failed, counts.Total)
	}
	return nil
}
