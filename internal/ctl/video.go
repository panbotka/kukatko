package ctl

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// The two per-video rebuilds, named to read as the thing being rebuilt — like the
// four in RebuildSpecs, and for the same reason. They are not in that list because
// neither is a rebuild the API runs on the spot: encoding a clip and cutting a
// sprite are the two most expensive things this application does, so both are
// scheduled and both answer with a step and its new state rather than a result.
const (
	// RebuildVideo prepares a video for playback: the streaming encode that cuts
	// it into the segments a browser plays, replacing whatever was encoded before.
	RebuildVideo = "video"
	// RebuildStoryboard re-cuts a video's scrub preview — the sprite of frames the
	// player shows along the timeline.
	RebuildStoryboard = "storyboard"
)

// hlsTranscodeStep is the processing report's name for the streaming encode, and
// therefore the path segment POST /photos/{uid}/process/{step} takes for it.
const hlsTranscodeStep = "hls_transcode"

// PhotoStep is one per-photo computation as the processing API reports it: which
// step, what state it is in now, when its evidence was recorded and what the last
// attempt failed with.
//
// It is deliberately the report's own shape rather than PhotoRebuild's: asking for
// a video to be prepared schedules work, so "queued" and "running" are the honest
// answers, and the caller's next question — has it landed yet? — is the one this
// shape already answers.
type PhotoStep struct {
	Step  string     `json:"step"`
	State string     `json:"state"`
	At    *time.Time `json:"at,omitempty"`
	Error string     `json:"error,omitempty"`
}

// VideoRendition is one encoded streaming quality of a video: what it is called,
// what picture it carries, what it asks of the connection and when it was made.
type VideoRendition struct {
	Rendition    string    `json:"rendition"`
	Width        int       `json:"width"`
	Height       int       `json:"height"`
	Bandwidth    int       `json:"bandwidth"`
	Codecs       string    `json:"codecs"`
	SegmentCount int       `json:"segment_count"`
	DurationMs   int       `json:"duration_ms"`
	EncodedAt    time.Time `json:"encoded_at"`
}

// videoRenditionsBody is the envelope GET /photos/{uid}/renditions answers with.
type videoRenditionsBody struct {
	Renditions []VideoRendition `json:"renditions"`
}

// Backfill is what a library-wide backfill endpoint answers with: how many jobs
// this call put in the queue. It is never how many finished — the work drains in
// the background, one clip at a time.
type Backfill struct {
	Enqueued int `json:"enqueued"`
}

// PrepareVideo schedules the streaming encode of one video and returns the
// server's raw JSON body, so `-o json` prints its own bytes. Decode it with
// DecodePhotoStep.
//
// It is POST /photos/{uid}/process/hls_transcode, and unlike every other step
// that endpoint schedules, this one really is a rebuild: the `hls_transcode`
// handler re-encodes the clip into every configured quality whatever is already
// recorded, replacing each rendition rather than adding to it. That is what makes
// it both the way to prepare a video the encoder never reached and the way to redo
// one encoded before a quality level was added or the segment length changed.
//
// It needs the maintainer role. The queue dedups per photo, so asking twice
// schedules one encode.
func (c *Client) PrepareVideo(ctx context.Context, photoUID string) (json.RawMessage, error) {
	if err := requireUID("photo", photoUID); err != nil {
		return nil, err
	}
	return c.send(ctx, http.MethodPost,
		"/photos/"+url.PathEscape(photoUID)+"/process/"+hlsTranscodeStep, nil)
}

// RebuildStoryboard discards a video's cached scrub-preview sprite and schedules a
// fresh one, returning the server's raw JSON body. Decode it with DecodePhotoStep.
//
// It is POST /photos/{uid}/regenerate-storyboard, and the discard is the whole
// point: the renderer is a no-op over a sprite that already exists, so a preview
// cut from an original that has since been corrected has no other way back. It
// needs the maintainer role.
func (c *Client) RebuildStoryboard(ctx context.Context, photoUID string) (json.RawMessage, error) {
	if err := requireUID("photo", photoUID); err != nil {
		return nil, err
	}
	return c.send(ctx, http.MethodPost,
		"/photos/"+url.PathEscape(photoUID)+"/regenerate-storyboard", nil)
}

// VideoRenditions returns what the streaming encode has produced for one video —
// which qualities exist, how big each picture is, what bitrate it demands, how
// many segments it was cut into and when it was encoded — as the server's raw
// JSON body. Decode it with DecodeVideoRenditions.
//
// A still, a video the encoder has not reached and an instance with streaming off
// all answer an empty list; an unknown uid is a 404. Any authenticated role may
// read it.
func (c *Client) VideoRenditions(ctx context.Context, photoUID string) (json.RawMessage, error) {
	if err := requireUID("photo", photoUID); err != nil {
		return nil, err
	}
	return c.get(ctx, "/photos/"+url.PathEscape(photoUID)+"/renditions", nil)
}

// BackfillHLS schedules the library-wide streaming encode and returns the
// server's raw JSON body. Decode it with DecodeBackfill.
//
// With all false it schedules only the videos that have never been encoded into a
// single rendition, which is the ordinary repair after a restore or an import.
// With all true it schedules every live video — a forced full re-encode, which is
// how a library picks up a newly configured quality level, and hours of background
// work on anything but a handful of clips.
//
// It only enqueues: the encode is the most expensive job in the queue and drains
// one clip at a time, so the number it answers with is jobs scheduled, never
// videos finished. It needs the maintainer role.
func (c *Client) BackfillHLS(ctx context.Context, all bool) (json.RawMessage, error) {
	path := "/process/hls"
	if all {
		path += "?all=true"
	}
	return c.send(ctx, http.MethodPost, path, nil)
}

// DecodePhotoStep decodes a step's reported state and stamps fallback onto it when
// the endpoint did not name the step itself.
func DecodePhotoStep(raw json.RawMessage, fallback string) (PhotoStep, error) {
	var step PhotoStep
	if err := json.Unmarshal(raw, &step); err != nil {
		return PhotoStep{}, fmt.Errorf("decoding the step: %w", err)
	}
	if step.Step == "" {
		step.Step = fallback
	}
	return step, nil
}

// DecodeVideoRenditions decodes the rendition inventory out of its envelope.
func DecodeVideoRenditions(raw json.RawMessage) ([]VideoRendition, error) {
	var body videoRenditionsBody
	if err := json.Unmarshal(raw, &body); err != nil {
		return nil, fmt.Errorf("decoding the renditions: %w", err)
	}
	return body.Renditions, nil
}

// DecodeBackfill decodes a backfill's count of scheduled jobs.
func DecodeBackfill(raw json.RawMessage) (Backfill, error) {
	var backfill Backfill
	if err := json.Unmarshal(raw, &backfill); err != nil {
		return Backfill{}, fmt.Errorf("decoding the backfill result: %w", err)
	}
	return backfill, nil
}

// WritePhotoStep renders one scheduled computation as a single line: what was
// asked for, and where it stands now that it has been asked for.
//
// It is written for the two commands that use it — both of which have just
// scheduled the work — and that is what makes the `done` case worth spelling out
// rather than printing. The processing report resolves persisted evidence before
// the queue, so a clip that was already encoded answers `done` with the *old*
// stamp even though this request really did queue a fresh encode behind it.
// Printing the state word alone there would read as "nothing happened", which is
// the opposite of what the command just did.
func WritePhotoStep(w io.Writer, step PhotoStep) error {
	return writeLine(w, step.Step+": "+describeStep(step))
}

// The step states the processing report answers with, spelled here because they
// are the wire contract rather than a Go value ctl could borrow.
const (
	stepQueued  = "queued"
	stepRunning = "running"
	stepDone    = "done"
	stepFailed  = "failed"
	stepSkipped = "skipped"
	stepPending = "pending"
)

// describeStep spells out a step's state in prose, saying what the caller should
// expect to happen next rather than repeating the state word alone.
func describeStep(step PhotoStep) string {
	switch step.State {
	case stepQueued:
		return "queued; the worker runs it in the background"
	case stepRunning:
		return "already running; the work you asked for is that run"
	case stepDone:
		return "the recorded result is the previous run" + doneAt(step.At) +
			"; the one you asked for is queued behind it"
	case stepFailed:
		return "the last attempt failed" + reason(step.Error) + "; a retry is in the queue"
	case stepSkipped:
		return "skipped; this step does not apply to this photo, or is switched off here"
	case stepPending:
		return "pending; nothing is scheduled for it"
	default:
		return dash(step.State)
	}
}

// doneAt appends when the recorded work landed, when the report says.
func doneAt(at *time.Time) string {
	if at == nil {
		return ""
	}
	return " (" + formatTime(at) + ")"
}

// reason appends the last attempt's error message, when there is one.
func reason(message string) string {
	if message == "" {
		return ""
	}
	return ": " + message
}

// WriteVideoRenditions renders a video's encoded qualities as a table, widest
// first, with a summary line carrying what they add up to. An unencoded clip
// prints one line and no header: an empty table would read like a failure, and
// "not encoded" is a normal state of a video.
func WriteVideoRenditions(w io.Writer, renditions []VideoRendition) error {
	if len(renditions) == 0 {
		return writeLine(w, "no streaming renditions: this photo has never been encoded")
	}
	rows := make([][]string, 0, len(renditions))
	for _, rendition := range renditions {
		rows = append(rows, []string{
			rendition.Rendition,
			formatDimensions(rendition.Width, rendition.Height),
			formatBitrate(rendition.Bandwidth),
			dash(rendition.Codecs),
			strconv.Itoa(rendition.SegmentCount),
			formatDuration(rendition.DurationMs),
			formatTime(&rendition.EncodedAt),
		})
	}
	header := []string{"RENDITION", "PICTURE", "BITRATE", "CODECS", "SEGMENTS", "LENGTH", "ENCODED"}
	if err := writeTable(w, header, rows); err != nil {
		return err
	}
	return writeLine(w, "\n"+renditionSummary(renditions))
}

// renditionSummary is the footer under the rendition table: how many qualities
// exist and when the oldest of them was encoded, which is the one an operator
// asking "is this still current?" has to look at.
func renditionSummary(renditions []VideoRendition) string {
	names := make([]string, 0, len(renditions))
	oldest := renditions[0].EncodedAt
	for _, rendition := range renditions {
		names = append(names, rendition.Rendition)
		if rendition.EncodedAt.Before(oldest) {
			oldest = rendition.EncodedAt
		}
	}
	return strconv.Itoa(len(renditions)) + " " + pluralRenditions(len(renditions)) +
		" (" + strings.Join(names, ", ") + ") · oldest encoded " + formatTime(&oldest)
}

// pluralRenditions picks the noun for a rendition count.
func pluralRenditions(count int) string {
	if count == 1 {
		return "rendition"
	}
	return "renditions"
}

// WriteBackfill renders a backfill's outcome as one line saying what was put in
// the queue — and, when nothing was, that this is a library with nothing left to
// do rather than a failure. what is the singular noun for one scheduled item
// ("video"), pluralised by the -s rule the callers' nouns all follow.
func WriteBackfill(w io.Writer, what string, backfill Backfill) error {
	if backfill.Enqueued == 0 {
		return writeLine(w, "nothing to do: no "+what+" is waiting for this work")
	}
	return writeLine(w, strconv.Itoa(backfill.Enqueued)+" "+pluralNoun(what, backfill.Enqueued)+
		" "+pluralWas(backfill.Enqueued)+" scheduled; the worker drains them in the background")
}

// pluralNoun picks the singular or the -s plural of what for a count.
func pluralNoun(what string, count int) string {
	if count == 1 {
		return what
	}
	return what + "s"
}

// pluralWas picks the verb for a scheduled count.
func pluralWas(count int) string {
	if count == 1 {
		return "was"
	}
	return "were"
}

// formatBitrate renders a bits-per-second figure in Mbit/s, the unit a video
// bitrate is actually talked about in, or a dash when it was not recorded.
func formatBitrate(bandwidth int) string {
	if bandwidth <= 0 {
		return "-"
	}
	return strconv.FormatFloat(float64(bandwidth)/1_000_000, 'f', 1, 64) + " Mbit/s"
}

// formatDuration renders a millisecond length as m:ss, or a dash when it is
// unknown. Hours are carried into the minutes rather than given their own field:
// a rendition of a two-hour clip is rare and "127:14" still reads.
func formatDuration(durationMs int) string {
	if durationMs <= 0 {
		return "-"
	}
	seconds := durationMs / 1000
	return strconv.Itoa(seconds/60) + ":" + fmt.Sprintf("%02d", seconds%60)
}
