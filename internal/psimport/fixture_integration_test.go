//go:build integration

package psimport_test

import (
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/pgvector/pgvector-go"
)

// psSchema is the search-path schema the fake photo-sorter fixture lives under,
// seeded alongside Kukátko's own (public-schema) tables in the single integration
// database so the read-only photosorter.Reader can be pointed at it.
const psSchema = "ps_fixture"

// createFixtureSQL (re)creates the fake photo-sorter schema and every table the
// migration reads, plus a photo-book and a share-link table that the migration
// must never touch. Vector columns are qualified to public so the pgvector type
// resolves regardless of the connection search_path. It is a const (not a
// function body) to keep the per-function line budget free for real logic.
const createFixtureSQL = `
DROP SCHEMA IF EXISTS ` + psSchema + ` CASCADE;
CREATE SCHEMA ` + psSchema + `;

CREATE TABLE ` + psSchema + `.photos (
	uid              TEXT PRIMARY KEY,
	file_hash        TEXT NOT NULL,
	file_path        TEXT NOT NULL,
	file_name        TEXT NOT NULL DEFAULT '',
	file_size        BIGINT NOT NULL DEFAULT 0,
	file_mime        TEXT NOT NULL DEFAULT '',
	file_width       INT NOT NULL DEFAULT 0,
	file_height      INT NOT NULL DEFAULT 0,
	file_orientation INT NOT NULL DEFAULT 1,
	taken_at         TIMESTAMPTZ,
	taken_at_source  TEXT NOT NULL DEFAULT 'unknown',
	title            TEXT NOT NULL DEFAULT '',
	description      TEXT NOT NULL DEFAULT '',
	notes            TEXT NOT NULL DEFAULT '',
	lat              DOUBLE PRECISION,
	lng              DOUBLE PRECISION,
	altitude         DOUBLE PRECISION,
	camera_make      TEXT NOT NULL DEFAULT '',
	camera_model     TEXT NOT NULL DEFAULT '',
	lens_model       TEXT NOT NULL DEFAULT '',
	iso              INT,
	aperture         DOUBLE PRECISION,
	exposure         TEXT NOT NULL DEFAULT '',
	focal_length     DOUBLE PRECISION,
	exif             JSONB,
	keywords         TEXT[] NOT NULL DEFAULT '{}',
	exif_artist      TEXT NOT NULL DEFAULT '',
	exif_copyright   TEXT NOT NULL DEFAULT '',
	exif_license     TEXT NOT NULL DEFAULT '',
	exif_software    TEXT NOT NULL DEFAULT '',
	scan             BOOLEAN NOT NULL DEFAULT false,
	panorama         BOOLEAN NOT NULL DEFAULT false,
	private          BOOLEAN NOT NULL DEFAULT false,
	archived_at      TIMESTAMPTZ,
	updated_at       TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE ` + psSchema + `.embeddings (
	photo_uid  TEXT PRIMARY KEY,
	embedding  public.vector(768) NOT NULL,
	model      TEXT NOT NULL DEFAULT '',
	pretrained TEXT NOT NULL DEFAULT ''
);

CREATE TABLE ` + psSchema + `.faces (
	photo_uid    TEXT NOT NULL,
	face_index   INT NOT NULL,
	embedding    public.vector(512) NOT NULL,
	bbox         DOUBLE PRECISION[] NOT NULL,
	det_score    DOUBLE PRECISION NOT NULL DEFAULT 0,
	model        TEXT,
	marker_uid   TEXT,
	subject_uid  TEXT,
	subject_name TEXT NOT NULL DEFAULT '',
	photo_width  INT NOT NULL DEFAULT 0,
	photo_height INT NOT NULL DEFAULT 0,
	orientation  INT NOT NULL DEFAULT 1,
	PRIMARY KEY (photo_uid, face_index)
);

CREATE TABLE ` + psSchema + `.faces_processed (
	photo_uid  TEXT PRIMARY KEY,
	face_count INT NOT NULL DEFAULT 0
);

CREATE TABLE ` + psSchema + `.subjects (
	uid      TEXT PRIMARY KEY,
	slug     TEXT NOT NULL,
	name     TEXT NOT NULL,
	type     TEXT NOT NULL DEFAULT 'person',
	favorite BOOLEAN NOT NULL DEFAULT false,
	private  BOOLEAN NOT NULL DEFAULT false,
	notes    TEXT NOT NULL DEFAULT ''
);

CREATE TABLE ` + psSchema + `.markers (
	uid         TEXT PRIMARY KEY,
	photo_uid   TEXT NOT NULL,
	subject_uid TEXT,
	type        TEXT NOT NULL DEFAULT 'face',
	x           DOUBLE PRECISION NOT NULL DEFAULT 0,
	y           DOUBLE PRECISION NOT NULL DEFAULT 0,
	w           DOUBLE PRECISION NOT NULL DEFAULT 0,
	h           DOUBLE PRECISION NOT NULL DEFAULT 0,
	score       INT NOT NULL DEFAULT 0,
	invalid     BOOLEAN NOT NULL DEFAULT false,
	reviewed    BOOLEAN NOT NULL DEFAULT false,
	created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE ` + psSchema + `.albums (
	uid         TEXT PRIMARY KEY,
	slug        TEXT NOT NULL,
	title       TEXT NOT NULL,
	description TEXT NOT NULL DEFAULT '',
	type        TEXT NOT NULL DEFAULT 'album',
	private     BOOLEAN NOT NULL DEFAULT false
);

CREATE TABLE ` + psSchema + `.album_photos (
	album_uid  TEXT NOT NULL,
	photo_uid  TEXT NOT NULL,
	sort_order INT NOT NULL DEFAULT 0,
	PRIMARY KEY (album_uid, photo_uid)
);

CREATE TABLE ` + psSchema + `.labels (
	uid      TEXT PRIMARY KEY,
	slug     TEXT NOT NULL,
	name     TEXT NOT NULL,
	priority INT NOT NULL DEFAULT 0
);

CREATE TABLE ` + psSchema + `.photo_labels (
	photo_uid   TEXT NOT NULL,
	label_uid   TEXT NOT NULL,
	source      TEXT NOT NULL DEFAULT 'manual',
	uncertainty INT NOT NULL DEFAULT 0,
	PRIMARY KEY (photo_uid, label_uid)
);

CREATE TABLE ` + psSchema + `.photo_phashes (
	photo_uid TEXT PRIMARY KEY,
	phash     BIGINT NOT NULL DEFAULT 0,
	dhash     BIGINT NOT NULL DEFAULT 0
);

CREATE TABLE ` + psSchema + `.photo_edits (
	photo_uid  TEXT PRIMARY KEY,
	crop_x     DOUBLE PRECISION,
	crop_y     DOUBLE PRECISION,
	crop_w     DOUBLE PRECISION,
	crop_h     DOUBLE PRECISION,
	rotation   INT NOT NULL DEFAULT 0,
	brightness DOUBLE PRECISION NOT NULL DEFAULT 0,
	contrast   DOUBLE PRECISION NOT NULL DEFAULT 0
);

-- Out-of-scope tables: present (with data) to prove the migration ignores them.
CREATE TABLE ` + psSchema + `.photobooks (
	uid   TEXT PRIMARY KEY,
	title TEXT NOT NULL
);

CREATE TABLE ` + psSchema + `.share_links (
	token     TEXT PRIMARY KEY,
	photo_uid TEXT NOT NULL
);
`

// psPhoto is the subset of a fake photo-sorter photos row the seeding helper
// sets; the remaining columns take their schema defaults.
type psPhoto struct {
	uid       string
	fileHash  string
	filePath  string
	fileName  string
	fileMime  string
	title     string
	takenAt   time.Time
	updatedAt time.Time
	// Credit/tag columns migration 036 added to photo-sorter; a zero value keeps
	// the schema default so existing callers stay unaffected.
	keywords  []string
	artist    string
	copyright string
	license   string
	software  string
	scan      bool
	panorama  bool
}

// setupFixtureSchema (re)creates the fake photo-sorter schema and its tables,
// failing the test on any error. It is called once per scenario after the
// public-schema tables have been truncated.
func setupFixtureSchema(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	if _, err := pool.Exec(t.Context(), createFixtureSQL); err != nil {
		t.Fatalf("creating fixture schema: %v", err)
	}
}

// seedPSPhoto inserts a fake photo-sorter photos row, carrying the credit/tag
// columns too so the migration's mapping of them can be asserted. A nil keywords
// slice is stored as the empty array the NOT NULL column requires.
func seedPSPhoto(t *testing.T, pool *pgxpool.Pool, p psPhoto) {
	t.Helper()
	keywords := p.keywords
	if keywords == nil {
		keywords = []string{}
	}
	const q = `INSERT INTO ` + psSchema + `.photos
		(uid, file_hash, file_path, file_name, file_mime, title, taken_at, updated_at,
		 keywords, exif_artist, exif_copyright, exif_license, exif_software, scan, panorama)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15)`
	exec(t, pool, q, p.uid, p.fileHash, p.filePath, p.fileName, p.fileMime, p.title, p.takenAt, p.updatedAt,
		keywords, p.artist, p.copyright, p.license, p.software, p.scan, p.panorama)
}

// seedPSEmbedding inserts a fake photo-sorter CLIP embedding row.
func seedPSEmbedding(t *testing.T, pool *pgxpool.Pool, photoUID string, vec []float32, model, pretrained string) {
	t.Helper()
	const q = `INSERT INTO ` + psSchema + `.embeddings (photo_uid, embedding, model, pretrained)
		VALUES ($1, $2, $3, $4)`
	exec(t, pool, q, photoUID, pgvector.NewVector(vec), model, pretrained)
}

// psFace is the subset of a fake photo-sorter faces row the seeding helper sets.
type psFace struct {
	photoUID   string
	faceIndex  int
	vec        []float32
	bbox       []float64
	detScore   float64
	model      string
	markerUID  *string
	subjectUID *string
	width      int
	height     int
}

// seedPSFace inserts a fake photo-sorter faces row plus its detection record so
// the migration treats the photo as processed.
func seedPSFace(t *testing.T, pool *pgxpool.Pool, f psFace) {
	t.Helper()
	const q = `INSERT INTO ` + psSchema + `.faces
		(photo_uid, face_index, embedding, bbox, det_score, model, marker_uid,
		 subject_uid, photo_width, photo_height, orientation)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, 1)`
	exec(t, pool, q, f.photoUID, f.faceIndex, pgvector.NewVector(f.vec), f.bbox,
		f.detScore, f.model, f.markerUID, f.subjectUID, f.width, f.height)
}

// seedPSFacesProcessed records that a fake photo-sorter photo was face-detected.
func seedPSFacesProcessed(t *testing.T, pool *pgxpool.Pool, photoUID string, count int) {
	t.Helper()
	const q = `INSERT INTO ` + psSchema + `.faces_processed (photo_uid, face_count) VALUES ($1, $2)`
	exec(t, pool, q, photoUID, count)
}

// seedPSSubject inserts a fake photo-sorter subjects row.
func seedPSSubject(t *testing.T, pool *pgxpool.Pool, uid, name, subjectType string) {
	t.Helper()
	const q = `INSERT INTO ` + psSchema + `.subjects (uid, slug, name, type) VALUES ($1, $2, $3, $4)`
	exec(t, pool, q, uid, name, name, subjectType)
}

// seedPSMarker inserts a fake photo-sorter markers row tied to a subject.
func seedPSMarker(t *testing.T, pool *pgxpool.Pool, uid, photoUID, subjectUID string) {
	t.Helper()
	const q = `INSERT INTO ` + psSchema + `.markers
		(uid, photo_uid, subject_uid, type, x, y, w, h, score, reviewed)
		VALUES ($1, $2, $3, 'face', 0.1, 0.1, 0.2, 0.2, 90, true)`
	exec(t, pool, q, uid, photoUID, subjectUID)
}

// seedPSAlbum inserts a fake photo-sorter albums row and one membership.
func seedPSAlbum(t *testing.T, pool *pgxpool.Pool, uid, title, photoUID string) {
	t.Helper()
	exec(t, pool, `INSERT INTO `+psSchema+`.albums (uid, slug, title, type) VALUES ($1, $2, $3, 'album')`,
		uid, title, title)
	exec(t, pool, `INSERT INTO `+psSchema+`.album_photos (album_uid, photo_uid, sort_order) VALUES ($1, $2, 0)`,
		uid, photoUID)
}

// seedPSLabel inserts a fake photo-sorter labels row and one attachment.
func seedPSLabel(t *testing.T, pool *pgxpool.Pool, uid, name, photoUID string) {
	t.Helper()
	exec(t, pool, `INSERT INTO `+psSchema+`.labels (uid, slug, name, priority) VALUES ($1, $2, $3, 5)`,
		uid, name, name)
	exec(t, pool, `INSERT INTO `+psSchema+`.photo_labels (photo_uid, label_uid, source) VALUES ($1, $2, 'ai')`,
		photoUID, uid)
}

// seedPSPhash inserts a fake photo-sorter perceptual-hash row.
func seedPSPhash(t *testing.T, pool *pgxpool.Pool, photoUID string, phash, dhash int64) {
	t.Helper()
	const q = `INSERT INTO ` + psSchema + `.photo_phashes (photo_uid, phash, dhash) VALUES ($1, $2, $3)`
	exec(t, pool, q, photoUID, phash, dhash)
}

// seedPSEdit inserts a fake photo-sorter non-destructive-edit row.
func seedPSEdit(t *testing.T, pool *pgxpool.Pool, photoUID string, rotation int) {
	t.Helper()
	const q = `INSERT INTO ` + psSchema + `.photo_edits (photo_uid, rotation, brightness, contrast)
		VALUES ($1, $2, 0.1, 0.2)`
	exec(t, pool, q, photoUID, rotation)
}

// seedIgnoredTables fills the out-of-scope photo-book and share-link tables so a
// successful run proves the migration never reads them.
func seedIgnoredTables(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	exec(t, pool, `INSERT INTO `+psSchema+`.photobooks (uid, title) VALUES ('bk1', 'My Book')`)
	exec(t, pool, `INSERT INTO `+psSchema+`.share_links (token, photo_uid) VALUES ('tok1', 'psp1')`)
}

// exec runs a fixture INSERT, failing the test on error.
func exec(t *testing.T, pool *pgxpool.Pool, sql string, args ...any) {
	t.Helper()
	if _, err := pool.Exec(t.Context(), sql, args...); err != nil {
		t.Fatalf("seeding fixture: %v", err)
	}
}

// unitVector builds a deterministic L2-normalised vector of length dim whose
// direction depends on seed, so distinct seeds give distinct (and recognisable)
// embeddings for similarity assertions.
func unitVector(dim, seed int) []float32 {
	vec := make([]float32, dim)
	for i := range vec {
		vec[i] = float32((i+seed)%7) + 1
	}
	var norm float64
	for _, v := range vec {
		norm += float64(v) * float64(v)
	}
	inv := float32(1.0)
	if norm > 0 {
		inv = float32(1.0 / sqrt(norm))
	}
	for i := range vec {
		vec[i] *= inv
	}
	return vec
}

// sqrt is a tiny Newton's-method square root, kept local so the fixture has no
// import-cycle-prone math dependency surprises in the test build.
func sqrt(x float64) float64 {
	if x == 0 {
		return 0
	}
	guess := x
	for range 40 {
		guess = (guess + x/guess) / 2
	}
	return guess
}
