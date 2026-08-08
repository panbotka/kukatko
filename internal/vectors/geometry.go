package vectors

import (
	"math"

	"github.com/panbotka/kukatko/internal/exif"
)

// FrameTransform names what the faces half of the orientation repair does with
// one face row whose cached frame is its photo's stored pixel pair transposed.
//
// It is an enumeration rather than one function because those rows are not all in
// the same coordinate space: whether the embeddings sidecar auto-rotated an image
// before detecting on it has varied over the library's life, and in the database a
// box that has to be turned a quarter turn is indistinguishable from one that only
// needs different divisors. The transform is therefore chosen per row from
// evidence (decideFaceBox) and the row is left alone when the evidence is silent —
// a face left where it is can still be repaired later, a face moved the wrong way
// twice cannot.
type FrameTransform int

const (
	// TransformSkip leaves the row exactly as it is, not even its cached frame. The
	// frame IS the fingerprint the repair finds the row by, so a row whose space the
	// evidence cannot establish has to keep it to be reconsidered by a later run.
	TransformSkip FrameTransform = iota
	// TransformFrame rewrites only the cached frame: the box is already in display
	// space and just the render hints held the frame swapped.
	TransformFrame
	// TransformRotate turns the box a quarter turn out of the raw (pre-rotation)
	// frame into the display frame. It is what the rows measured against the live
	// catalogue needed: their pixel box came from the untouched original and was
	// divided by the (equally untouched) raw pair, which leaves a box that is right
	// in the raw frame and a quarter turn away from the displayed one.
	TransformRotate
	// TransformRescale re-divides the box per axis by rawWidth/rawHeight and its
	// reciprocal. It repairs the other provenance — a pixel box the sidecar had
	// already auto-rotated into display space, divided by the raw pair — where the
	// two frames coincide up to a scale and nothing has to move.
	TransformRescale
)

// candidateTransforms are the corrections the repair may apply, i.e. every
// transform except leaving the row alone.
var candidateTransforms = [...]FrameTransform{TransformFrame, TransformRotate, TransformRescale}

// The evidence thresholds a candidate transform is judged by.
//
// maxCentreDistance is how close a transformed box's centre has to come to a
// marker's to count as landing on that face: 5 % of the frame is well inside a
// face box, and an order of magnitude below the ~0.3–0.5 by which a wrong
// transform displaces a box (the production case measured 0.019 for the right
// transform and 0.29 for the nearest wrong one). decisiveRatio and minSeparation
// are the margin the runner-up has to lose by — both, so neither a near-tie at
// tiny distances nor a proportional tie at large ones is read as a decision.
// frameSlack is how far outside the frame a transformed box may still fall before
// the candidate counts as refuted; a detector box legitimately overhangs the image
// by a little, a box in the wrong space misses it by a lot.
const (
	maxCentreDistance = 0.05
	decisiveRatio     = 2.0
	minSeparation     = 0.02
	frameSlack        = 0.02
)

// TransposedFace is one face row whose cached frame is its photo's stored pixel
// pair transposed — the fingerprint of the orientation defect — together with the
// photo geometry the decision needs.
type TransposedFace struct {
	// PhotoUID and FaceIndex identify the row.
	PhotoUID  string
	FaceIndex int
	// BBox is the normalised box as stored, in whichever space it was recorded.
	BBox [4]float64
	// Orientation is the photo's EXIF orientation tag (5–8 here, the quarter turns).
	Orientation int
	// RawWidth and RawHeight are the photo's stored (pre-rotation) dimensions, i.e.
	// what photos.file_width/file_height mean.
	RawWidth  int
	RawHeight int
}

// FaceBoxPlan is the decision for one TransposedFace: which transform the evidence
// supports and the box that follows from it.
type FaceBoxPlan struct {
	// Face is the row the plan is for.
	Face TransposedFace
	// Transform is the correction to apply; TransformSkip means write nothing at all.
	Transform FrameTransform
	// BBox is the row's box after Transform — the stored box unchanged for
	// TransformSkip and TransformFrame.
	BBox [4]float64
}

// repairable reports whether the row is one the repair can reason about at all: a
// quarter turn over a known, non-square frame. A square frame is excluded because
// every candidate transform collapses into the same box there, so there is nothing
// to decide and nothing to gain.
func (f TransposedFace) repairable() bool {
	return exif.QuarterTurn(f.Orientation) &&
		f.RawWidth > 0 && f.RawHeight > 0 && f.RawWidth != f.RawHeight
}

// transformed returns the row's box after t.
func (f TransposedFace) transformed(t FrameTransform) [4]float64 {
	switch t {
	case TransformRotate:
		return RotateRawBBox(f.BBox, f.Orientation)
	case TransformRescale:
		return RenormalizeTransposedBBox(f.BBox, f.RawWidth, f.RawHeight, f.Orientation)
	default:
		return f.BBox
	}
}

// RotateRawBBox maps a normalised box out of the RAW (pre-rotation) frame into the
// display frame of a quarter-turned photo, which is the raw frame with the sides
// exchanged. The box's width and height therefore swap and its position turns with
// the image, following the EXIF orientation's own definition: 5 transposes, 6
// turns 90° clockwise, 7 transverses and 8 turns 270° clockwise.
//
// This is the correction a box needs when the detector's pixels were read off the
// untouched original (no auto-rotation) and divided by that original's own
// dimensions: the numbers are then a correct box in a frame nobody ever displays.
// Anything but a quarter turn leaves the box alone — there the two frames coincide.
func RotateRawBBox(bbox [4]float64, orientation int) [4]float64 {
	x, y, w, h := bbox[0], bbox[1], bbox[2], bbox[3]
	switch orientation {
	case 5: // Transpose: mirrored, then 270° clockwise.
		return [4]float64{y, x, h, w}
	case 6: // 90° clockwise.
		return [4]float64{1 - (y + h), x, h, w}
	case 7: // Transverse: mirrored, then 90° clockwise.
		return [4]float64{1 - (y + h), 1 - (x + w), h, w}
	case 8: // 270° clockwise.
		return [4]float64{y, 1 - (x + w), h, w}
	default:
		return bbox
	}
}

// RenormalizeTransposedBBox re-divides a face bbox per axis by rawWidth/rawHeight
// and its reciprocal. It is the correction for a box whose pixels were ALREADY in
// display space — a sidecar that auto-rotates before detecting produces those —
// but which was divided by the raw frame: only the divisors were wrong, x and w
// were divided by the raw width where the display width applies and y and h by the
// raw height where the display height applies. Nothing moves, the box is only
// rescaled.
//
// It is one candidate of the faces repair, not its rule. Measured against the live
// catalogue the rows needed RotateRawBBox instead (4 detection/marker pairs on 3
// quarter-turned photos reconciled under the rotation, none under this rescale),
// so it is applied only to a row whose own evidence points at it.
//
// Anything but a quarter turn leaves the box alone: without a swap the two frames
// coincide and the normalisation was already right. A degenerate frame is likewise
// returned unchanged rather than producing NaN/Inf coordinates.
func RenormalizeTransposedBBox(bbox [4]float64, rawWidth, rawHeight, orientation int) [4]float64 {
	if !exif.QuarterTurn(orientation) || rawWidth <= 0 || rawHeight <= 0 {
		return bbox
	}
	scale := float64(rawWidth) / float64(rawHeight)
	return [4]float64{bbox[0] * scale, bbox[1] / scale, bbox[2] * scale, bbox[3] / scale}
}

// planFaceBoxes decides what to do with every candidate face row of ONE photo,
// given that photo's marker boxes (normalised, display space — the coordinates the
// viewer draws and a person has looked at).
//
// Each row is decided on its own evidence first. A row the markers cannot place
// then inherits the verdict of its photo, but only when every decided row of the
// photo agrees on one transform: a photo's face rows are written by a single
// detection run (RecordFaceDetection replaces them wholesale), so they share a
// coordinate space, and a photo with more detections than markers would otherwise
// end up half repaired — one box on the face and the rest still turned. Rows of a
// photo whose evidence is silent or contradictory keep TransformSkip.
func planFaceBoxes(faces []TransposedFace, markers [][4]float64) []FaceBoxPlan {
	plans := make([]FaceBoxPlan, len(faces))
	for i, face := range faces {
		t := decideFaceBox(face, markers)
		plans[i] = FaceBoxPlan{Face: face, Transform: t, BBox: face.transformed(t)}
	}
	shared, ok := unanimousTransform(plans)
	if !ok {
		return plans
	}
	for i, plan := range plans {
		if plan.Transform != TransformSkip || !plan.Face.repairable() {
			continue
		}
		plans[i].Transform = shared
		plans[i].BBox = plan.Face.transformed(shared)
	}
	return plans
}

// unanimousTransform returns the one transform every decided row of a photo agrees
// on, and whether there is one. Disagreement (two rows placed in different spaces)
// and a photo with nothing decided both yield false, so nothing is inherited.
func unanimousTransform(plans []FaceBoxPlan) (FrameTransform, bool) {
	agreed := TransformSkip
	for _, plan := range plans {
		if plan.Transform == TransformSkip {
			continue
		}
		if agreed != TransformSkip && agreed != plan.Transform {
			return TransformSkip, false
		}
		agreed = plan.Transform
	}
	return agreed, agreed != TransformSkip
}

// decideFaceBox returns the transform one row's own evidence supports, or
// TransformSkip when it supports none.
//
// The evidence is the photo's markers: a marker is a region a person has seen sit
// on a face in the displayed image, so the candidate that brings the detection
// onto a marker is the one whose coordinate space matches the marker's. A
// candidate that would put the box off the photo is refuted before it is scored.
// The winner has to land on a marker (maxCentreDistance) and beat the runner-up by
// a clear margin; without markers, with an ambiguous field, or on a row the repair
// cannot reason about, nothing is decided and the row is left alone.
func decideFaceBox(face TransposedFace, markers [][4]float64) FrameTransform {
	if !face.repairable() {
		return TransformSkip
	}
	best, runnerUp := math.Inf(1), math.Inf(1)
	winner := TransformSkip
	for _, t := range candidateTransforms {
		box := face.transformed(t)
		distance, ok := nearestMarkerDistance(box, markers)
		if !ok || !insideFrame(box) {
			continue
		}
		switch {
		case distance < best:
			best, runnerUp, winner = distance, best, t
		case distance < runnerUp:
			runnerUp = distance
		}
	}
	if best > maxCentreDistance || runnerUp < max(best*decisiveRatio, best+minSeparation) {
		return TransformSkip
	}
	return winner
}

// nearestMarkerDistance returns the distance from box's centre to the nearest
// marker's centre, and whether there was any marker to measure against. Centres,
// not overlap: a detection and a marker of the same face routinely differ in size
// (the two came from different detectors), while their centres coincide.
func nearestMarkerDistance(box [4]float64, markers [][4]float64) (float64, bool) {
	nearest := math.Inf(1)
	for _, marker := range markers {
		if d := centreDistance(box, marker); d < nearest {
			nearest = d
		}
	}
	return nearest, len(markers) > 0
}

// centreDistance returns the distance between the centres of two normalised boxes,
// in fractions of the frame.
func centreDistance(a, b [4]float64) float64 {
	return math.Hypot((a[0]+a[2]/2)-(b[0]+b[2]/2), (a[1]+a[3]/2)-(b[1]+b[3]/2))
}

// insideFrame reports whether a normalised box lands on the photo, allowing
// frameSlack of overhang and rejecting a box with no area.
func insideFrame(b [4]float64) bool {
	return b[2] > 0 && b[3] > 0 &&
		b[0] >= -frameSlack && b[1] >= -frameSlack &&
		b[0]+b[2] <= 1+frameSlack && b[1]+b[3] <= 1+frameSlack
}
