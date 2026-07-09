package main

import (
	"encoding/json"
	"fmt"
	"io"

	"github.com/panbotka/kukatko/internal/ctl"
)

// renderRaw writes one API response in the requested format: the server's own
// JSON bytes, unchanged, or a compact table decoded from them.
//
// Every resource passes its own decoder and renderer, because the API has no
// uniform list envelope — /photos wraps its rows in a paging envelope, /albums,
// /labels and /subjects return bare lists, and /photos/bulk a result summary. The
// shapes are what the frontend consumes, so ctl adapts to them rather than the
// other way round; this generic only removes the plumbing they do share.
//
// what names the resource in an error message ("album list", "subject").
func renderRaw[T any](
	w io.Writer, format ctl.Format, raw json.RawMessage, what string,
	decode func(json.RawMessage) (T, error),
	write func(io.Writer, T) error,
) error {
	if format == ctl.FormatJSON {
		return writeRawJSON(w, raw)
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
func renderAck(w io.Writer, format ctl.Format, message string) error {
	if err := ctl.WriteAck(w, format, message); err != nil {
		return fmt.Errorf("writing the confirmation: %w", err)
	}
	return nil
}

// renderAlbums writes the bare {"albums": […]} list.
func renderAlbums(w io.Writer, format ctl.Format, raw json.RawMessage) error {
	return renderRaw(w, format, raw, "album list", ctl.DecodeAlbums, ctl.WriteAlbums)
}

// renderAlbum writes one album, as returned by the detail and create endpoints.
func renderAlbum(w io.Writer, format ctl.Format, raw json.RawMessage) error {
	return renderRaw(w, format, raw, "album", ctl.DecodeAlbum, ctl.WriteAlbum)
}

// renderMembership writes an album's refreshed photo order after a membership
// mutation, as one summary line naming the album.
func renderMembership(w io.Writer, format ctl.Format, raw json.RawMessage, albumUID string) error {
	return renderRaw(w, format, raw, "album membership", ctl.DecodePhotoUIDs,
		func(w io.Writer, uids []string) error { return ctl.WriteMembership(w, albumUID, uids) })
}

// renderLabels writes the bare {"labels": […]} list.
func renderLabels(w io.Writer, format ctl.Format, raw json.RawMessage) error {
	return renderRaw(w, format, raw, "label list", ctl.DecodeLabels, ctl.WriteLabels)
}

// renderLabel writes one label, as returned by the detail and create endpoints.
func renderLabel(w io.Writer, format ctl.Format, raw json.RawMessage) error {
	return renderRaw(w, format, raw, "label", ctl.DecodeLabel, ctl.WriteLabel)
}

// renderSubjects writes the bare {"subjects": […]} list.
func renderSubjects(w io.Writer, format ctl.Format, raw json.RawMessage) error {
	return renderRaw(w, format, raw, "subject list", ctl.DecodeSubjects, ctl.WriteSubjects)
}

// renderSubject writes one subject.
func renderSubject(w io.Writer, format ctl.Format, raw json.RawMessage) error {
	return renderRaw(w, format, raw, "subject", ctl.DecodeSubject, ctl.WriteSubject)
}

// renderBulkResult writes a bulk edit's per-photo outcome.
func renderBulkResult(w io.Writer, format ctl.Format, raw json.RawMessage) error {
	return renderRaw(w, format, raw, "bulk result", ctl.DecodeBulkResult, ctl.WriteBulkResult)
}
