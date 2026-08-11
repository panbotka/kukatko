package embedding

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
)

// OCRBlock is one recognised region of text: what it says, where it sits in the
// image and how sure the recogniser is. The bounding box is in source pixels as
// [x_min, y_min, x_max, y_max].
//
// Kukátko does not persist the boxes — there is no consumer for them yet — but
// the client returns them because the service does, and dropping data at the
// transport layer would make a future overlay a client change rather than a
// storage one.
type OCRBlock struct {
	// Text is the block's recognised text.
	Text string
	// BBox is the block's bounding box in source pixels: [x1, y1, x2, y2].
	BBox [4]float64
	// Confidence is the recogniser's score for this block, 0..1.
	Confidence float64
}

// OCRResult is what the sidecar read from one image: the blocks joined into one
// string in reading order, the blocks themselves, and the tags identifying which
// recogniser produced them.
//
// An image with no text is a normal, successful result whose Text is empty and
// whose Blocks are nil — "we looked and there was nothing" is an answer, not a
// failure, and callers must be able to record it as one.
type OCRResult struct {
	// Text is every block joined by newlines, in reading order.
	Text string
	// Blocks are the individual recognised regions, in the same order.
	Blocks []OCRBlock
	// Lang is the recogniser's language/script tag, e.g. "latin".
	Lang string
	// Model is the recogniser's model tag, e.g. "PP-OCRv5_mobile".
	Model string
}

// ocrBlockItem is a single block entry in ocrEnvelope.
type ocrBlockItem struct {
	Text       string    `json:"text"`
	BBox       []float64 `json:"bbox"`
	Confidence float64   `json:"confidence"`
}

// ocrEnvelope is the /ocr/image response body.
type ocrEnvelope struct {
	Text        string         `json:"text"`
	BlocksCount int            `json:"blocks_count"`
	Blocks      []ocrBlockItem `json:"blocks"`
	Lang        string         `json:"lang"`
	Model       string         `json:"model"`
}

// ImageOCR reads the text printed in the image streamed from img by POSTing a
// multipart "file" part to /ocr/image. A positive minConfidence is sent along as
// the min_confidence field; a non-positive one is omitted so the service's own
// default applies.
//
// It is bounded by the same request timeout as image embedding — this is queue
// work on the same possibly cold GPU — and classifies failures identically, so
// an offline box surfaces as the retryable ErrUnavailable and the caller can
// requeue instead of burning an attempt. An image with no text at all comes back
// as a successful, empty OCRResult.
func (c *HTTPClient) ImageOCR(ctx context.Context, img io.Reader, minConfidence float64) (OCRResult, error) {
	var fields map[string]string
	if minConfidence > 0 {
		fields = map[string]string{"min_confidence": strconv.FormatFloat(minConfidence, 'f', -1, 64)}
	}
	body, err := c.postMultipartFields(ctx, endpointOCR, img, fields)
	if err != nil {
		return OCRResult{}, err
	}
	var resp ocrEnvelope
	if err := json.Unmarshal(body, &resp); err != nil {
		return OCRResult{}, fmt.Errorf("%s: %w: %w", endpointOCR, ErrBadResponse, err)
	}
	blocks, err := toOCRBlocks(resp.Blocks)
	if err != nil {
		return OCRResult{}, err
	}
	return OCRResult{Text: resp.Text, Blocks: blocks, Lang: resp.Lang, Model: resp.Model}, nil
}

// toOCRBlocks validates and converts the wire blocks. A block whose bounding box
// is not four numbers is ErrBadResponse: the service either speaks the documented
// contract or it does not, and silently keeping a block with no geometry would
// hide a version skew.
func toOCRBlocks(items []ocrBlockItem) ([]OCRBlock, error) {
	if len(items) == 0 {
		return nil, nil
	}
	out := make([]OCRBlock, 0, len(items))
	for i, b := range items {
		if len(b.BBox) != 4 {
			return nil, fmt.Errorf(
				"%s: %w: block %d bbox has %d values, want 4", endpointOCR, ErrBadResponse, i, len(b.BBox))
		}
		out = append(out, OCRBlock{
			Text:       b.Text,
			BBox:       [4]float64{b.BBox[0], b.BBox[1], b.BBox[2], b.BBox[3]},
			Confidence: b.Confidence,
		})
	}
	return out, nil
}
