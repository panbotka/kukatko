package main

import (
	"encoding/json"
	"fmt"
	"io"

	"github.com/panbotka/kukatko/internal/ctl"
)

// renderRaw writes one API response in the requested output mode: the server's
// own JSON bytes unchanged, the same bytes slimmed for an agent, or a compact
// table decoded from them.
//
// Every resource passes its own decoder and renderer, because the API has no
// uniform list envelope — /photos wraps its rows in a paging envelope, /albums,
// /labels and /subjects return bare lists, and /photos/bulk a result summary. The
// shapes are what the frontend consumes, so ctl adapts to them rather than the
// other way round; this generic only removes the plumbing they do share.
//
// The llm mode is handled here, before the decoder runs, which is what makes it
// available to every command rather than to the ones somebody remembered: it is a
// rule about JSON keys, not about any one resource.
//
// what names the resource in an error message ("album list", "subject").
func renderRaw[T any](
	w io.Writer, out ctl.Output, raw json.RawMessage, what string,
	decode func(json.RawMessage) (T, error),
	write func(io.Writer, T) error,
) error {
	if out.Format == ctl.FormatJSON {
		return writeRawJSON(w, raw)
	}
	if out.Format == ctl.FormatLLM {
		if err := ctl.WriteLLM(w, raw, out.Fields); err != nil {
			return fmt.Errorf("rendering the %s: %w", what, err)
		}
		return nil
	}
	value, err := decode(raw)
	if err != nil {
		return fmt.Errorf("rendering the %s: %w", what, err)
	}
	if err := write(w, value); err != nil {
		return fmt.Errorf("rendering the %s: %w", what, err)
	}
	return nil
}

// renderAck confirms a mutation whose endpoint answered 204 No Content, so there
// are no server bytes to pass through. See ctl.WriteAck.
func renderAck(w io.Writer, out ctl.Output, message string) error {
	if err := ctl.WriteAck(w, out, message); err != nil {
		return fmt.Errorf("writing the confirmation: %w", err)
	}
	return nil
}

// renderRendition confirms a file saved by `photos image`.
func renderRendition(w io.Writer, out ctl.Output, saved ctl.Rendition) error {
	if err := ctl.WriteRendition(w, out, saved); err != nil {
		return fmt.Errorf("writing the saved rendition: %w", err)
	}
	return nil
}

// renderAlbums writes the bare {"albums": […]} list.
func renderAlbums(w io.Writer, out ctl.Output, raw json.RawMessage) error {
	return renderRaw(w, out, raw, "album list", ctl.DecodeAlbums, ctl.WriteAlbums)
}

// renderAlbum writes one album, as returned by the detail and create endpoints.
func renderAlbum(w io.Writer, out ctl.Output, raw json.RawMessage) error {
	return renderRaw(w, out, raw, "album", ctl.DecodeAlbum, ctl.WriteAlbum)
}

// renderMembership writes an album's refreshed photo order after a membership
// mutation, as one summary line naming the album.
func renderMembership(w io.Writer, out ctl.Output, raw json.RawMessage, albumUID string) error {
	return renderRaw(w, out, raw, "album membership", ctl.DecodePhotoUIDs,
		func(w io.Writer, uids []string) error { return ctl.WriteMembership(w, albumUID, uids) })
}

// renderLabels writes the bare {"labels": […]} list.
func renderLabels(w io.Writer, out ctl.Output, raw json.RawMessage) error {
	return renderRaw(w, out, raw, "label list", ctl.DecodeLabels, ctl.WriteLabels)
}

// renderLabel writes one label, as returned by the detail and create endpoints.
func renderLabel(w io.Writer, out ctl.Output, raw json.RawMessage) error {
	return renderRaw(w, out, raw, "label", ctl.DecodeLabel, ctl.WriteLabel)
}

// renderSubjects writes the bare {"subjects": […]} list.
func renderSubjects(w io.Writer, out ctl.Output, raw json.RawMessage) error {
	return renderRaw(w, out, raw, "subject list", ctl.DecodeSubjects, ctl.WriteSubjects)
}

// renderSubject writes one subject.
func renderSubject(w io.Writer, out ctl.Output, raw json.RawMessage) error {
	return renderRaw(w, out, raw, "subject", ctl.DecodeSubject, ctl.WriteSubject)
}

// renderBulkResult writes a bulk edit's per-photo outcome.
func renderBulkResult(w io.Writer, out ctl.Output, raw json.RawMessage) error {
	return renderRaw(w, out, raw, "bulk result", ctl.DecodeBulkResult, ctl.WriteBulkResult)
}

// renderFaceList writes a photo's detections with their markers and suggestions.
func renderFaceList(w io.Writer, out ctl.Output, raw json.RawMessage) error {
	return renderRaw(w, out, raw, "face list", ctl.DecodeFaceList, ctl.WriteFaceList)
}

// renderFaceAssign writes the outcome of one face assignment or detachment.
func renderFaceAssign(w io.Writer, out ctl.Output, raw json.RawMessage) error {
	return renderRaw(w, out, raw, "assignment", ctl.DecodeFaceAssign, ctl.WriteFaceAssign)
}

// renderClusters writes the bare {"clusters": […]} list of unnamed face groups.
func renderClusters(w io.Writer, out ctl.Output, raw json.RawMessage) error {
	return renderRaw(w, out, raw, "cluster list", ctl.DecodeClusters, ctl.WriteClusters)
}

// renderClusterAssign writes the outcome of naming a whole cluster.
func renderClusterAssign(w io.Writer, out ctl.Output, raw json.RawMessage) error {
	return renderRaw(w, out, raw, "cluster assignment", ctl.DecodeClusterAssign, ctl.WriteClusterAssign)
}

// renderClusterRemoval writes what removing a stray face left of a cluster.
func renderClusterRemoval(w io.Writer, out ctl.Output, raw json.RawMessage, clusterUID string) error {
	return renderRaw(w, out, raw, "cluster", ctl.DecodeClusterRemoval,
		func(w io.Writer, cluster *ctl.Cluster) error {
			return ctl.WriteClusterRemoval(w, clusterUID, cluster)
		})
}

// renderMergeReport writes what a merge of two subjects moved onto the keeper,
// naming both of them.
func renderMergeReport(w io.Writer, out ctl.Output, report ctl.MergeReport) error {
	if err := ctl.WriteMergeReport(w, out, report); err != nil {
		return fmt.Errorf("writing the merge result: %w", err)
	}
	return nil
}
