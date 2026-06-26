package video

import (
	"context"
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/panbotka/kukatko/internal/exif"
)

// probeWithExiftool reads video metadata via the exif package's exiftool path
// (the shared time/GPS/dimension fields) and augments it with the video-only
// fields — duration, frame rate, codecs — pulled from the raw exiftool tag
// document. It is the fallback used when ffprobe is not installed.
func probeWithExiftool(ctx context.Context, path string) (Metadata, error) {
	em, err := exif.Extract(ctx, path)
	if err != nil {
		return Metadata{}, fmt.Errorf("video: exiftool fallback: %w", err)
	}
	meta := Metadata{
		TakenAt:  em.TakenAt,
		Lat:      em.Lat,
		Lng:      em.Lng,
		Altitude: em.Altitude,
		Width:    em.Width,
		Height:   em.Height,
		Mime:     em.Mime,
		Raw:      em.Exif,
	}
	applyExiftoolVideoFields(&meta, em.Exif)
	return meta, nil
}

// applyExiftoolVideoFields fills the video-specific fields (duration, frame
// rate, video/audio codecs) from an exiftool tag object produced with numeric
// (-n) output. Missing tags simply leave their fields at the zero value.
func applyExiftoolVideoFields(meta *Metadata, obj map[string]any) {
	if obj == nil {
		return
	}
	if secs, ok := tagFloat(obj, "Duration", "MediaDuration", "TrackDuration"); ok && secs > 0 {
		ms := int(math.Round(secs * 1000))
		meta.DurationMs = &ms
	}
	if fps, ok := tagFloat(obj, "VideoFrameRate", "FrameRate"); ok && fps > 0 {
		meta.FPS = &fps
	}
	meta.VideoCodec = tagString(obj, "CompressorID", "VideoCodec", "CompressorName")
	if audio := tagString(obj, "AudioFormat", "AudioCodec"); audio != "" {
		meta.AudioCodec = audio
		meta.HasAudio = true
	}
	if !meta.HasAudio {
		if ch, ok := tagFloat(obj, "AudioChannels"); ok && ch > 0 {
			meta.HasAudio = true
		}
	}
}

// tagString returns the first non-empty string value among keys in the exiftool
// tag object, trimming surrounding whitespace.
func tagString(obj map[string]any, keys ...string) string {
	for _, key := range keys {
		if s, ok := obj[key].(string); ok {
			if s = strings.TrimSpace(s); s != "" {
				return s
			}
		}
	}
	return ""
}

// tagFloat returns the first numeric value among keys in the exiftool tag
// object, accepting JSON numbers and numeric strings.
func tagFloat(obj map[string]any, keys ...string) (float64, bool) {
	for _, key := range keys {
		switch v := obj[key].(type) {
		case float64:
			return v, true
		case string:
			if f, err := strconv.ParseFloat(strings.TrimSpace(v), 64); err == nil {
				return f, true
			}
		}
	}
	return 0, false
}
