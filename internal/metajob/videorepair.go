package metajob

import (
	"math"

	"github.com/panbotka/kukatko/internal/exif"
	"github.com/panbotka/kukatko/internal/photos"
	"github.com/panbotka/kukatko/internal/video"
)

// fpsEpsilon is how far two frame rates may differ before the probe's value is
// treated as a correction rather than the same reading. A stored rate is the
// double the same division produced, so anything above float noise is a genuine
// difference.
const fpsEpsilon = 1e-6

// planVideoRepair decides, field by field, what a fresh probe of a video's
// container may write back onto the row the catalogue holds. It is pure: it reads
// the photo and the probe and returns the decision, so the rules below are the
// whole policy and can be read (and tested) as such.
//
// The file-derived fields — duration, frame size, frame rate, video codec, and
// the audio pair — are the container's own business. Nothing but a probe ever
// writes them, so a probe may *correct* them: a value that differs from what the
// file says is offered, an unchanged one is not (which is what makes a re-probe of
// a healthy clip a no-op). A field the probe could not read is never offered,
// because "I did not see it" is not the same statement as "it is not there".
//
// The curated fields — capture time and location — belong to whoever last decided
// about them. They are offered only to fill a gap:
//
//   - a capture time only when the photo has none, or has one that was merely
//     guessed from its file name. A time somebody typed ("manual"), a time an
//     earlier probe read out of the file ("exif"), a date deliberately declared
//     unknown (taken_at_before_unknown holds what was put away) and a date marked
//     as an estimate are all left exactly as they are;
//   - coordinates only when the photo has none at all *and* nobody has decided
//     anything about its location (an empty location_source). This is the rule the
//     location estimator already follows, and it is what makes "manual" with no
//     coordinates the tombstone it is meant to be: a location somebody deleted is
//     never handed back.
//
// A probe that read nothing at all returns the zero VideoRepair, i.e. "change
// nothing".
func planVideoRepair(photo photos.Photo, vm video.Metadata) photos.VideoRepair {
	// A probe that read no container at all states nothing, and its zero values must
	// not be mistaken for statements: without this guard a second failed probe would
	// "correct" a clip's duration to unknown and its sound to silence — the very
	// damage this repair exists to undo.
	if !vm.HasContainerMetadata() {
		return photos.VideoRepair{}
	}
	repair := photos.VideoRepair{
		DurationMs: repairInt(photo.DurationMs, vm.DurationMs),
		FPS:        repairFloat(photo.FPS, vm.FPS, fpsEpsilon),
		VideoCodec: repairString(photo.VideoCodec, vm.VideoCodec),
	}
	applyDimensions(&repair, photo, vm)
	applyAudio(&repair, photo, vm)
	applyTakenAt(&repair, photo, vm)
	applyLocation(&repair, photo, vm)
	return repair
}

// applyDimensions offers the probed frame size when the container states one and
// it differs from the stored pair. Width and height travel together: a video's
// frame is one fact, and repairing half of it would produce an aspect ratio that
// belongs to no file.
func applyDimensions(repair *photos.VideoRepair, photo photos.Photo, vm video.Metadata) {
	if vm.Width <= 0 || vm.Height <= 0 {
		return
	}
	if vm.Width == photo.FileWidth && vm.Height == photo.FileHeight {
		return
	}
	repair.Width = &vm.Width
	repair.Height = &vm.Height
}

// applyAudio offers the audio pair when it differs from what the catalogue holds.
// Both columns move together: whether there is sound and what encoded it are one
// reading, and a clip that turns out to be silent must not keep the codec name of
// a stream it does not have.
func applyAudio(repair *photos.VideoRepair, photo photos.Photo, vm video.Metadata) {
	if vm.HasAudio == photo.HasAudio && vm.AudioCodec == photo.AudioCodec {
		return
	}
	codec := vm.AudioCodec
	hasAudio := vm.HasAudio
	repair.AudioCodec = &codec
	repair.HasAudio = &hasAudio
}

// applyTakenAt offers the container's creation time only for a photo whose date
// is missing or was merely guessed from its file name, and never for one somebody
// has decided about. See planVideoRepair for the whole rule.
func applyTakenAt(repair *photos.VideoRepair, photo photos.Photo, vm video.Metadata) {
	if vm.TakenAt == nil || !takenAtIsFillable(photo) {
		return
	}
	if photo.TakenAt != nil && photo.TakenAt.Equal(*vm.TakenAt) {
		return
	}
	when := vm.TakenAt.UTC()
	repair.TakenAt = &when
	repair.TakenAtSource = string(exif.SourceExif)
}

// takenAtIsFillable reports whether the photo's capture time is a gap a probe may
// fill: no date at all or one guessed from the file name, with nobody's decision
// recorded over it — not a manual edit, not a date declared unknown (which leaves
// the disowned value in taken_at_before_unknown), not one flagged as an estimate.
func takenAtIsFillable(photo photos.Photo) bool {
	if photo.TakenAtSource == photos.TakenAtSourceManual || photo.TakenAtEstimated {
		return false
	}
	if photo.TakenAtBeforeUnknown != nil {
		return false
	}
	return photo.TakenAt == nil || photo.TakenAtSource == string(exif.SourceFilename)
}

// applyLocation offers the container's coordinates only to a photo that has none
// and whose location nobody has decided about, and the altitude only when it is
// missing. Latitude and longitude travel together: half a fix is not a location.
func applyLocation(repair *photos.VideoRepair, photo photos.Photo, vm video.Metadata) {
	if photo.Altitude == nil && vm.Altitude != nil {
		altitude := *vm.Altitude
		repair.Altitude = &altitude
	}
	if vm.Lat == nil || vm.Lng == nil {
		return
	}
	if photo.Lat != nil || photo.Lng != nil || photo.LocationSource != "" {
		return
	}
	lat, lng := *vm.Lat, *vm.Lng
	repair.Lat = &lat
	repair.Lng = &lng
	repair.LocationSource = photos.LocationSourceExif
}

// repairInt offers probed when the container states a positive value that differs
// from the stored one, and nil otherwise.
func repairInt(stored, probed *int) *int {
	if probed == nil || *probed <= 0 {
		return nil
	}
	if stored != nil && *stored == *probed {
		return nil
	}
	value := *probed
	return &value
}

// repairFloat offers probed when the container states a positive value that
// differs from the stored one by more than epsilon, and nil otherwise.
func repairFloat(stored, probed *float64, epsilon float64) *float64 {
	if probed == nil || *probed <= 0 {
		return nil
	}
	if stored != nil && math.Abs(*stored-*probed) <= epsilon {
		return nil
	}
	value := *probed
	return &value
}

// repairString offers probed when the container names something other than what
// is stored, and nil otherwise. An empty reading is never offered: a probe that
// did not name a codec has not established that there is none.
func repairString(stored, probed string) *string {
	if probed == "" || probed == stored {
		return nil
	}
	return &probed
}
