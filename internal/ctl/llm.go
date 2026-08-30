package ctl

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"slices"
	"strings"
)

// droppedKeys are the JSON keys the llm format removes wherever they appear.
//
// The rule behind the list is one question: would a reader that has never seen
// this photo learn anything from the value? Everything here fails it, and every
// one of them is expensive in tokens:
//
//   - exif is a raw camera document, hundreds of tags deep, none of which the
//     catalogue does not already surface in a named column;
//   - thumb_url and download_url are long signed URLs that expire — an agent
//     fetches the image with `photos image`, not by pasting a URL back;
//   - file_hash, file_path, file_orientation and the whole files list describe
//     where the bytes live, which is the storage layer's business;
//   - software, color_profile, image_codec, camera_serial, original_name,
//     projection, video_codec, audio_codec and title_edited are machine-derived
//     provenance the API itself refuses to edit;
//   - processing is the queue's bookkeeping about the photo, not the photo;
//   - the photoprism/photosorter ids record which library a row came from;
//   - created_at, updated_at and metadata_extracted_at are row bookkeeping — the
//     date that means something is taken_at, which is kept.
var droppedKeys = map[string]bool{
	"exif":                  true,
	"thumb_url":             true,
	"download_url":          true,
	"file_hash":             true,
	"file_path":             true,
	"file_orientation":      true,
	"files":                 true,
	"processing":            true,
	"software":              true,
	"color_profile":         true,
	"image_codec":           true,
	"camera_serial":         true,
	"original_name":         true,
	"projection":            true,
	"video_codec":           true,
	"audio_codec":           true,
	"title_edited":          true,
	"photoprism_uid":        true,
	"photoprism_file_hash":  true,
	"photosorter_uid":       true,
	"created_at":            true,
	"updated_at":            true,
	"metadata_extracted_at": true,
}

// WriteLLM renders one API response the way an agent should read it: compact
// JSON with everything that costs tokens and teaches nothing removed — the keys
// in droppedKeys, and every empty or zero-valued field: a zero rating and an
// empty note say exactly as much as the field not being there at all.
//
// It is deliberately generic over the response shape rather than a projection
// per resource: the API has no uniform envelope (see renderRaw), so a rule about
// keys applies to a photo, an album, a subject and anything added later, while a
// hand-written projection would only ever cover what someone remembered.
//
// fields, when non-empty, narrows the result further to that allowlist; see
// pickFields for how it reaches into the list envelopes.
func WriteLLM(w io.Writer, raw json.RawMessage, fields []string) error {
	decoded, err := decodeAny(raw)
	if err != nil {
		return err
	}
	slimmed, kept := slim(decoded)
	if !kept {
		slimmed = map[string]any{}
	}
	if len(fields) > 0 {
		picked, ok := pickFields(slimmed, fields)
		if !ok {
			picked = map[string]any{}
		}
		slimmed = picked
	}
	encoded, err := json.Marshal(slimmed)
	if err != nil {
		return fmt.Errorf("encoding the llm output: %w", err)
	}
	return WriteJSON(w, encoded)
}

// decodeAny decodes a response body into plain Go values, keeping numbers as
// json.Number so a count is re-encoded exactly as the server wrote it rather
// than through a float.
func decodeAny(raw json.RawMessage) (any, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var decoded any
	if err := dec.Decode(&decoded); err != nil {
		return nil, fmt.Errorf("decoding the response for llm output: %w", err)
	}
	return decoded, nil
}

// slim strips one decoded value, reporting whether anything survived. A dropped
// key, a null, an empty string, a zero number, a false boolean and a container
// left with no members are all "nothing survived", so their key disappears from
// the parent object.
func slim(value any) (any, bool) {
	switch typed := value.(type) {
	case map[string]any:
		return slimObject(typed)
	case []any:
		return slimArray(typed)
	case string:
		return typed, typed != ""
	case json.Number:
		return typed, !isZeroNumber(typed)
	case bool:
		return typed, typed
	case nil:
		return nil, false
	default:
		// float64 only reaches here for a value decoded without UseNumber, which
		// WriteLLM never does; keep it rather than silently dropping a number.
		return typed, true
	}
}

// slimObject strips every value of an object and drops the denied keys.
func slimObject(object map[string]any) (map[string]any, bool) {
	out := make(map[string]any, len(object))
	for key, value := range object {
		if droppedKeys[key] {
			continue
		}
		slimmed, kept := slim(value)
		if kept {
			out[key] = slimmed
		}
	}
	return out, len(out) > 0
}

// slimArray strips every element of an array, dropping the ones that lost
// everything — an array of empty objects is no more informative than no array.
func slimArray(array []any) ([]any, bool) {
	out := make([]any, 0, len(array))
	for _, element := range array {
		slimmed, kept := slim(element)
		if kept {
			out = append(out, slimmed)
		}
	}
	return out, len(out) > 0
}

// isZeroNumber reports whether a JSON number is zero, however it was written
// (0, 0.0, -0, 0e3). An unparseable number is not treated as zero: it is the
// server's own bytes and dropping it would be a lie.
func isZeroNumber(number json.Number) bool {
	value, err := number.Float64()
	return err == nil && value == 0
}

// pickFields narrows a slimmed value to the named keys, reporting whether
// anything survived.
//
// A named key keeps its whole value. A key that was not named survives only as a
// road to one that was: an object or an array is kept when filtering it leaves
// something behind, so `--fields uid,title` reaches through the `photos` envelope
// of a listing without the caller having to name the envelope.
func pickFields(value any, fields []string) (any, bool) {
	switch typed := value.(type) {
	case map[string]any:
		return pickObject(typed, fields)
	case []any:
		return pickArray(typed, fields)
	default:
		return value, false
	}
}

// pickObject keeps the named keys of an object whole and every other key only
// when filtering its value leaves something behind.
func pickObject(object map[string]any, fields []string) (map[string]any, bool) {
	out := make(map[string]any, len(object))
	for key, nested := range object {
		if slices.Contains(fields, key) {
			out[key] = nested
			continue
		}
		if picked, ok := pickFields(nested, fields); ok {
			out[key] = picked
		}
	}
	return out, len(out) > 0
}

// pickArray filters every element of an array, dropping the ones nothing
// survived in.
func pickArray(array []any, fields []string) ([]any, bool) {
	out := make([]any, 0, len(array))
	for _, element := range array {
		if picked, ok := pickFields(element, fields); ok {
			out = append(out, picked)
		}
	}
	return out, len(out) > 0
}

// ParseFields splits the --fields value into an allowlist, trimming blanks and
// dropping empties, so `--fields "uid, title,"` means the two keys it names.
func ParseFields(raw string) []string {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	fields := make([]string, 0, len(parts))
	for _, part := range parts {
		if trimmed := strings.TrimSpace(part); trimmed != "" {
			fields = append(fields, trimmed)
		}
	}
	return fields
}
