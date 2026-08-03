package query

import "strings"

// EntityKind names the kind of thing a UID identifies. It is the routing
// decision a pasted id makes possible: every UID Kukátko mints carries a
// two-letter prefix that says what it is, so an id can be resolved against
// exactly one table with no guessing and no fan-out.
type EntityKind string

// The entity kinds a UID can name. The values double as the wire strings of
// the global search's direct hit, so they are the vocabulary the frontend
// routes on.
const (
	// EntityPhoto is a photo row (uid prefix "ph").
	EntityPhoto EntityKind = "photo"
	// EntityAlbum is an album (uid prefix "al").
	EntityAlbum EntityKind = "album"
	// EntityLabel is a label (uid prefix "lb").
	EntityLabel EntityKind = "label"
	// EntityPerson is a subject — a person, animal or other named entity
	// (uid prefix "su"). It is called "person" here because that is what the
	// API and the UI call the group.
	EntityPerson EntityKind = "person"
	// EntityStack is a stack, the group of files of one shot (uid prefix "st").
	// It resolves to the stack's primary photo.
	EntityStack EntityKind = "stack"
	// EntityMarker is a marker — a face/region on one photo (uid prefix "mk").
	// It resolves to the photo it sits on.
	EntityMarker EntityKind = "marker"
	// EntityPhotoprism is a PhotoPrism source photo uid (prefix "pt", and 16
	// characters rather than 26). The library came from PhotoPrism and the
	// external ids are stored on purpose, so an id copied out of PhotoPrism
	// while debugging the migration resolves to the catalogue row that holds it.
	EntityPhotoprism EntityKind = "photoprism"
)

// nativeUIDPrefixes maps the two-letter prefix of a UID minted by Kukátko to
// the entity it names. Every one of them is 26 characters long; the PhotoPrism
// prefix is handled separately because its ids are shorter and drawn from a
// wider alphabet.
var nativeUIDPrefixes = map[string]EntityKind{
	"ph": EntityPhoto,
	"al": EntityAlbum,
	"lb": EntityLabel,
	"su": EntityPerson,
	"st": EntityStack,
	"mk": EntityMarker,
}

const (
	// nativeUIDLen is the total length of a UID minted by Kukátko: a
	// two-character prefix plus 24 random base32 characters.
	nativeUIDLen = 26
	// nativeUIDAlphabet is the 32-symbol lowercase alphabet those random
	// characters are drawn from (see internal/photos uidAlphabet).
	nativeUIDAlphabet = "0123456789abcdefghijklmnopqrstuv"
	// photoprismUIDPrefix marks a PhotoPrism source photo uid.
	photoprismUIDPrefix = "pt"
	// photoprismUIDLen is the total length of a PhotoPrism uid, which is
	// deliberately different from nativeUIDLen so the two can never collide.
	photoprismUIDLen = 16
)

// UIDRef is a UID recognised in a search input together with what it names.
type UIDRef struct {
	// UID is the recognised id, lowercased (UIDs are lowercase by construction,
	// so lowercasing only rescues an id that was shouted or auto-capitalised).
	UID string
	// Kind is the entity the id names.
	Kind EntityKind
}

// FindUID returns the first UID-shaped token in input, so pasting a bare id —
// or an id with a word next to it, the way it arrives out of a log line — is
// recognised as a lookup. ok is false when no token is shaped like a UID, which
// is the common case of ordinary text and costs one cheap scan.
//
// Only a token with a known prefix qualifies: a bare id of the right length but
// with an unknown prefix is NOT probed against every table, because that would
// be several queries per keystroke of a search-as-you-type box for no benefit.
func FindUID(input string) (UIDRef, bool) {
	for field := range strings.FieldsSeq(input) {
		if ref, ok := ClassifyUID(field); ok {
			return ref, true
		}
	}
	return UIDRef{}, false
}

// ClassifyUID reports which entity a single token names, or ok=false when the
// token is not shaped like a UID at all. The shapes are disjoint: Kukátko's own
// ids are 26 characters of lowercase base32 behind a known two-letter prefix,
// PhotoPrism's are 16 characters of lowercase base36 behind "pt".
func ClassifyUID(token string) (UIDRef, bool) {
	uid := strings.ToLower(token)
	switch len(uid) {
	case nativeUIDLen:
		kind, known := nativeUIDPrefixes[uid[:2]]
		if !known || !isAlphabet(uid[2:], nativeUIDAlphabet) {
			return UIDRef{}, false
		}
		return UIDRef{UID: uid, Kind: kind}, true
	case photoprismUIDLen:
		if !strings.HasPrefix(uid, photoprismUIDPrefix) || !isBase36(uid[2:]) {
			return UIDRef{}, false
		}
		return UIDRef{UID: uid, Kind: EntityPhotoprism}, true
	default:
		return UIDRef{}, false
	}
}

// isAlphabet reports whether every byte of s is in the given alphabet.
func isAlphabet(s, alphabet string) bool {
	for i := range len(s) {
		if !strings.ContainsRune(alphabet, rune(s[i])) {
			return false
		}
	}
	return true
}

// isBase36 reports whether every byte of s is a lowercase base36 digit, the
// alphabet PhotoPrism draws its uids from.
func isBase36(s string) bool {
	for i := range len(s) {
		c := s[i]
		if (c < '0' || c > '9') && (c < 'a' || c > 'z') {
			return false
		}
	}
	return true
}
