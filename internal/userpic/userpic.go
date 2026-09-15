// Package userpic is a user's profile picture: the small square that stands for
// an account wherever a person is named — beside a comment, on the navbar's
// account control — instead of the coloured initial the app drew before.
//
// # The chain
//
// A picture is not one thing but the first of four answers that exists, and the
// order is the point:
//
//  1. a picture the user uploaded, re-encoded on receipt (Source.Upload);
//  2. a photograph of the library the user pointed at (Source.PhotoUID);
//  3. the avatar of the person users.subject_uid says the account is — the
//     *default*, so an account that has said who it is wears that face with no
//     action from anybody (Source.PhotoUID plus Source.Face);
//  4. nothing, which the HTTP layer answers 404 and the client draws as the
//     coloured initial.
//
// The chain is resolved here, on the server, and never by the client: a caller
// asks one endpoint and either gets a picture or a 404, and which of the three
// sources answered is nobody else's business. Falling *through* rather than
// failing is what makes it robust — a picked photo that is archived, purged or
// flagged private later is simply not an answer any more, and the account wears
// the face below it instead of a broken image.
//
// # What is stored
//
// An upload is kept in Postgres (table user_pictures), as a square JPEG of at
// most MaxSide pixels that this package encodes itself; the submitted bytes are
// never retained. A pick stores only the photo's uid and is rendered through the
// same centre-crop renderer that cuts a subject's avatar, so no pixels are
// copied. Neither adds a prefix to the object store, which is why backup,
// storage migration and the library wipe need to know nothing about any of this.
//
// # What may be picked
//
// A photo flagged private or hidden from the library may not become a profile
// picture (ErrPhotoNotAllowed). Those flags exist so a photograph stays out of
// what everybody browses; putting one on a profile — where it is shown to every
// reader of every thread the account has written in — would be the plainest way
// to sidestep them.
//
// # Not audited
//
// None of these writes is audited, following POST /auth/password: the audit
// trail records what was done *to* an account by somebody else, and a person
// changing their own profile is not that.
package userpic

import (
	"errors"
	"time"
)

// Kind is which of the two stored answers a row holds. The third source of the
// chain — the linked subject's face — is never stored, so it has no Kind: it is
// what the resolver falls through to.
type Kind string

const (
	// KindUpload is a picture the user uploaded, re-encoded and kept as bytes.
	KindUpload Kind = "upload"
	// KindPhoto is a photograph of the library, kept as a uid reference.
	KindPhoto Kind = "photo"
)

// Sentinel errors returned by this package so callers (the HTTP layer, tests)
// can branch with errors.Is.
var (
	// ErrNoPicture indicates the account has no picture from any source in the
	// chain. It is an ordinary answer rather than a failure — most accounts are
	// in exactly this state, and it is the client's cue to draw the initial.
	ErrNoPicture = errors.New("userpic: user has no picture")
	// ErrUnsupportedFormat indicates the uploaded bytes are not one of the
	// formats the library decodes without CGO (JPEG, PNG, WebP), or are not a
	// decodable image at all.
	ErrUnsupportedFormat = errors.New("userpic: unsupported image format")
	// ErrTooLarge indicates the upload exceeded MaxUploadBytes. It is reported
	// before the request is read into memory, so an oversized body costs a
	// rejection rather than the allocation it asks for.
	ErrTooLarge = errors.New("userpic: uploaded picture is too large")
	// ErrPhotoNotFound indicates the picked photo does not exist in the library.
	ErrPhotoNotFound = errors.New("userpic: no such photo")
	// ErrPhotoNotAllowed indicates the picked photo is flagged private or hidden
	// from the library, which bars it from becoming anybody's profile picture.
	ErrPhotoNotAllowed = errors.New("userpic: a private or hidden photo cannot be a profile picture")
)

// MaxUploadBytes is the largest upload accepted, before decoding. A profile
// picture is a ~512 px square once this package is done with it, so anything a
// camera or a phone produces fits several times over; the bound exists so a
// request cannot ask the server to buffer an arbitrary amount of memory on the
// strength of being signed in.
const MaxUploadBytes int64 = 8 << 20

// Picture is one stored row: which answer the user gave, and the answer itself.
// Exactly one of Image and PhotoUID is set, as the table's CHECK enforces.
type Picture struct {
	// Kind says which of Image and PhotoUID carries the answer.
	Kind Kind
	// Image is the re-encoded square JPEG, for KindUpload.
	Image []byte
	// PhotoUID names the library photo, for KindPhoto. It is a bare uid with no
	// foreign key behind it, so it may name a photo that no longer exists.
	PhotoUID string
	// UpdatedAt is when the picture was last set.
	UpdatedAt time.Time
}

// Source is what the resolved chain hands the HTTP layer: either bytes to serve
// as they are, or a photo (and optionally a face box) to render through the
// avatar renderer. Exactly one of Upload and PhotoUID is set.
type Source struct {
	// Upload holds the stored JPEG bytes when the answer is an uploaded picture.
	Upload []byte
	// PhotoUID names the photo to cut the picture from, for a picked photo or an
	// inherited subject avatar.
	PhotoUID string
	// Face is the normalised face box to cut, set only when the answer is the
	// linked subject's detected face. Nil means the photo is shown whole,
	// centre-cropped square — which is what a picked photo and a hand-chosen
	// subject cover both want.
	Face *Box
}

// Box is a normalised [x, y, w, h] rectangle in a photo's display space, each
// value in 0..1 — the same geometry people.Box and avatar.Box carry, repeated
// here so this package depends on neither of them in its signatures.
type Box struct {
	X float64
	Y float64
	W float64
	H float64
}
