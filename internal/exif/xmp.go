package exif

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"encoding/xml"
	"io"
	"os"
	"strconv"
	"strings"
)

// The JPEG markers the XMP scan has to know: an APP1 segment may carry the XMP
// packet, and the entropy-coded scan (SOS) or the end of the image (EOI) is where
// the segment headers stop and there is nothing left to look at.
const (
	jpegMarkerPrefix byte = 0xFF
	jpegSOI          byte = 0xD8
	jpegAPP1         byte = 0xE1
	jpegSOS          byte = 0xDA
	jpegEOI          byte = 0xD9
)

// xmpSignature prefixes the payload of the APP1 segment that holds an XMP packet
// (XMP specification part 3), and is what tells it apart from the EXIF APP1 that
// uses the same marker.
const xmpSignature = "http://ns.adobe.com/xap/1.0/\x00"

// tiffNamespace is the XMP namespace the Orientation property belongs to; it
// mirrors the TIFF/EXIF tag of the same name and carries the same 1-8 values.
const tiffNamespace = "http://ns.adobe.com/tiff/1.0/"

// orientationProperty is the local name of the property inside tiffNamespace.
const orientationProperty = "Orientation"

// xmpOrientation reads tiff:Orientation out of the XMP packet of the JPEG at
// path, returning 1-8 or 0 when there is nothing to read: not a JPEG, no XMP
// packet, no orientation in it, or a malformed one. It never fails loudly — the
// caller's question is "does the file say anything", and a broken packet says
// nothing.
//
// It exists because a file can carry its orientation ONLY in XMP: the batch that
// exposed this were JPEGs with no EXIF block at all and tiff:Orientation="8" in
// their packet, which the EXIF-only reader called upright and sent to the face
// detector lying on its side.
func xmpOrientation(path string) int {
	// G304: path is the storage/imgconvert file the caller is about to read.
	file, err := os.Open(path) //nolint:gosec
	if err != nil {
		return 0
	}
	defer func() { _ = file.Close() }()

	packet := jpegXMPPacket(file)
	if len(packet) == 0 {
		return 0
	}
	return packetOrientation(packet)
}

// jpegXMPPacket walks the JPEG segment headers of r and returns the payload of
// the first APP1 segment carrying an XMP packet, or nil when there is none. The
// standard library's image/jpeg discards APP segments, so the markers have to be
// walked here.
//
// The walk stops at the start of the scan (or the end of the image): everything
// after it is entropy-coded data, not segment headers. Anything unexpected —
// truncated file, a length that cannot hold its own field, bytes that are not a
// JPEG at all — ends the walk with nil rather than an error or a panic.
func jpegXMPPacket(r io.Reader) []byte {
	buffered := bufio.NewReader(r)
	if !readSOI(buffered) {
		return nil
	}
	for {
		marker, ok := nextMarker(buffered)
		if !ok || marker == jpegSOS || marker == jpegEOI {
			return nil
		}
		payload, ok := segmentPayload(buffered)
		if !ok {
			return nil
		}
		if marker == jpegAPP1 && bytes.HasPrefix(payload, []byte(xmpSignature)) {
			return payload[len(xmpSignature):]
		}
	}
}

// readSOI consumes the start-of-image marker and reports whether it was there,
// which is the cheapest way to tell a JPEG from anything else that reached this
// reader (a PNG, a RAW, a truncated download).
func readSOI(r *bufio.Reader) bool {
	var start [2]byte
	if _, err := io.ReadFull(r, start[:]); err != nil {
		return false
	}
	return start[0] == jpegMarkerPrefix && start[1] == jpegSOI
}

// nextMarker reads the next segment marker: a 0xFF byte, any number of 0xFF fill
// bytes, then the marker itself. It reports false at the end of the file or when
// the byte where a marker must begin is not 0xFF, which means the file's segment
// structure is broken and the walk cannot continue.
func nextMarker(r *bufio.Reader) (byte, bool) {
	b, err := r.ReadByte()
	if err != nil || b != jpegMarkerPrefix {
		return 0, false
	}
	for {
		b, err = r.ReadByte()
		if err != nil {
			return 0, false
		}
		if b != jpegMarkerPrefix {
			return b, true
		}
	}
}

// segmentPayload reads one segment's big-endian length field and the payload it
// announces. It reports false for a length that cannot even hold its own field
// and for a payload the file is too short for, so a truncated segment is a dead
// end rather than a short read nobody notices.
func segmentPayload(r *bufio.Reader) ([]byte, bool) {
	var header [2]byte
	if _, err := io.ReadFull(r, header[:]); err != nil {
		return nil, false
	}
	length := binary.BigEndian.Uint16(header[:])
	if length < 2 {
		return nil, false
	}
	payload := make([]byte, length-2)
	if _, err := io.ReadFull(r, payload); err != nil {
		return nil, false
	}
	return payload, true
}

// packetOrientation returns the tiff:Orientation of an XMP packet, or 0 when the
// packet does not carry a usable one. Both serialisations count: the attribute
// form (tiff:Orientation="8" on an rdf:Description) and the element form
// (<tiff:Orientation>8</tiff:Orientation>), because a writer may choose either
// and the same picture must read the same way whichever it chose.
//
// Parsing is non-strict and every error simply ends the scan: an XMP packet is
// foreign data and a malformed one has to say "no orientation", never fail.
func packetOrientation(packet []byte) int {
	decoder := xml.NewDecoder(bytes.NewReader(packet))
	decoder.Strict = false
	for {
		token, err := decoder.Token()
		if err != nil {
			return 0
		}
		element, ok := token.(xml.StartElement)
		if !ok {
			continue
		}
		if orientation, ok := attributeOrientation(element); ok {
			return orientation
		}
		if orientation, ok := elementOrientation(decoder, element); ok {
			return orientation
		}
	}
}

// elementOrientation returns the orientation held as the text of the element
// itself, and whether there was one. A property whose text is not a usable
// orientation is not one, so the scan simply carries on.
func elementOrientation(decoder *xml.Decoder, element xml.StartElement) (int, bool) {
	if !isOrientationName(element.Name) {
		return 0, false
	}
	var text string
	if err := decoder.DecodeElement(&text, &element); err != nil {
		return 0, false
	}
	return parseOrientation(text)
}

// attributeOrientation returns the orientation held by one of the element's
// attributes, and whether there was one.
func attributeOrientation(element xml.StartElement) (int, bool) {
	for _, attr := range element.Attr {
		if !isOrientationName(attr.Name) {
			continue
		}
		if orientation, ok := parseOrientation(attr.Value); ok {
			return orientation, true
		}
	}
	return 0, false
}

// isOrientationName reports whether name is tiff:Orientation. The namespace is
// accepted both resolved (the packet declared xmlns:tiff, which is the normal
// case) and unresolved as the bare prefix, so a packet that omits the declaration
// still reads rather than silently returning nothing.
func isOrientationName(name xml.Name) bool {
	return name.Local == orientationProperty &&
		(name.Space == tiffNamespace || name.Space == "tiff")
}

// parseOrientation turns an XMP property value into an EXIF orientation, and
// reports false for anything that is not one of the eight defined values.
func parseOrientation(value string) (int, bool) {
	orientation, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil || orientation < 1 || orientation > 8 {
		return 0, false
	}
	return orientation, true
}
