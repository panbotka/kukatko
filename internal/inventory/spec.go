// Package inventory takes a deterministic, file-based snapshot of the catalogue —
// row counts, the schema's columns, per-table content digests, the photo and
// account rosters and (optionally) which originals the store is missing — and
// compares two snapshots. It is the before/after check of an upgrade that
// migrates data: take one snapshot before the deploy, one after, and diff them;
// every difference must be one the upgrade was designed to make.
//
// A snapshot must be comparable across a schema change, so the content digests
// cover an explicit column allow-list per table (columns that exist both before
// and after the multi-tenancy migrations), never "every column". The collector
// is strictly read-only — it runs in one READ ONLY REPEATABLE READ transaction —
// and it never applies migrations, so the new binary can inventory a database
// still on the old schema.
package inventory

// File names inside a snapshot directory. Every file is tab-separated, sorted
// and ends with a newline, so two snapshots of the same data are byte-identical.
const (
	// CountsFile lists every table of the schema with its row count.
	CountsFile = "counts.tsv"
	// ColumnsFile lists every column of every table with its type.
	ColumnsFile = "columns.tsv"
	// DigestsFile lists the digested tables: row count, digest, absent columns.
	DigestsFile = "digests.tsv"
	// PhotosFile lists every photo: uid, file_hash, file_path, file_size, archived_at.
	PhotosFile = "photos.tsv"
	// UsersFile lists every account: uid, username, role, disabled.
	UsersFile = "users.tsv"
	// StorageMissingFile lists the originals the store lacks or holds with the
	// wrong size; it is written only by a storage check, and empty means good.
	StorageMissingFile = "storage-missing.tsv"
)

// digestSpec names one digested table: the columns hashed per row (in this
// order) and the key the rows are ordered by. Every column existed at schema
// version 0099 (the last release before multi-tenancy) and still exists after
// the tenancy migrations; project_id, last_project_uid and anything else those
// migrations add are left out on purpose, so a before/after digest pair is
// equal exactly when the pre-existing data is.
type digestSpec struct {
	table   string
	orderBy []string
	columns []string
}

// digestSpecs is the allow-list of digested tables and columns. Two deliberate
// omissions: photos.fts is a generated column (derived from the others), and
// users.role is left to users.tsv, where the diff can check the designed
// collapse of the old roles to "user" row by row instead of failing the whole
// digest.
var digestSpecs = []digestSpec{
	{"album_photos", []string{"album_uid", "photo_uid"}, []string{"album_uid", "photo_uid", "added_at"}},
	{"albums", []string{"uid"}, []string{
		"uid", "slug", "title", "description", "type", "cover_photo_uid", "private", "created_by",
		"created_at", "updated_at", "featured_until", "featured_at", "featured_by",
	}},
	{"api_tokens", []string{"id"}, []string{
		"id", "user_uid", "name", "secret_hash", "created_at", "expires_at", "last_used_at",
		"revoked_at", "unlimited",
	}},
	{"comments", []string{"uid"}, []string{
		"uid", "photo_uid", "author_uid", "body", "created_at", "edited_at", "deleted_at", "task_uid",
		"album_uid",
	}},
	{"embeddings", []string{"photo_uid"}, []string{
		"photo_uid", "embedding", "model", "pretrained", "dim", "created_at",
	}},
	{"faces", []string{"id"}, []string{
		"id", "photo_uid", "face_index", "embedding", "bbox", "det_score", "model", "dim", "created_at",
		"marker_uid", "subject_uid", "subject_name", "photo_width", "photo_height", "orientation",
		"cluster_uid",
	}},
	{"labels", []string{"uid"}, []string{
		"uid", "slug", "name", "priority", "created_at", "updated_at", "review_enabled",
	}},
	{"markers", []string{"uid"}, []string{
		"uid", "photo_uid", "subject_uid", "type", "x", "y", "w", "h", "score", "invalid", "reviewed",
		"created_at", "updated_at",
	}},
	{"passkey_credentials", []string{"id"}, []string{
		"id", "user_uid", "credential_id", "public_key", "sign_count", "transports", "aaguid",
		"attestation_type", "attestation_format", "backup_eligible", "backup_state", "user_present",
		"user_verified", "name", "created_at", "last_used_at",
	}},
	{"photo_edits", []string{"photo_uid"}, []string{
		"photo_uid", "crop_x", "crop_y", "crop_w", "crop_h", "rotation", "brightness", "contrast",
		"updated_at",
	}},
	{"photo_files", []string{"id"}, []string{
		"id", "photo_uid", "file_path", "file_hash", "file_size", "file_mime", "is_primary", "role",
		"created_at",
	}},
	{"photo_hls_renditions", []string{"photo_uid", "rendition"}, []string{
		"photo_uid", "rendition", "playlist", "width", "height", "bandwidth", "codecs", "segment_count",
		"duration_ms", "encoded_at",
	}},
	{"photo_labels", []string{"photo_uid", "label_uid"}, []string{
		"photo_uid", "label_uid", "source", "uncertainty", "created_at",
	}},
	{"photo_places", []string{"photo_uid"}, []string{
		"photo_uid", "country", "region", "city", "place_name", "lat", "lng", "geocoded_at",
	}},
	{"photo_task_photos", []string{"task_uid", "photo_uid"}, []string{"task_uid", "photo_uid", "added_at"}},
	{"photo_tasks", []string{"uid"}, []string{
		"uid", "title", "body", "state", "resolution", "source_query", "created_by", "created_at",
		"updated_at", "state_at", "closed_at", "closed_by", "state_by", "options",
	}},
	{"photoprism_aliases", []string{"photoprism_uid"}, []string{
		"photoprism_uid", "photo_uid", "photoprism_file_hash", "created_at",
	}},
	{"photos", []string{"uid"}, []string{
		"uid", "file_hash", "file_path", "file_name", "file_size", "file_mime", "file_width",
		"file_height", "file_orientation", "taken_at", "taken_at_source", "title", "description", "notes",
		"lat", "lng", "altitude", "camera_make", "camera_model", "lens_model", "iso", "aperture",
		"exposure", "focal_length", "exif", "private", "archived_at", "uploaded_by", "photoprism_uid",
		"photoprism_file_hash", "photosorter_uid", "created_at", "updated_at", "media_type",
		"duration_ms", "video_codec", "audio_codec", "has_audio", "fps", "storage_migrated_at",
		"ai_note", "subject", "artist", "copyright", "license", "keywords", "software", "scan",
		"color_profile", "image_codec", "camera_serial", "original_name", "projection",
		"metadata_extracted_at", "taken_at_estimated", "taken_at_note", "stack_uid", "stack_primary",
		"location_source", "sidecar_written_at", "title_edited", "hidden_from_library",
		"taken_at_precision", "ocr_text", "ocr_at", "ocr_model", "taken_at_before_unknown", "blurhash",
		"thumbnails_at",
	}},
	{"saved_searches", []string{"uid"}, []string{
		"uid", "owner_uid", "name", "params", "created_at", "updated_at",
	}},
	{"subject_families", []string{"uid"}, []string{
		"uid", "partner_a_uid", "partner_b_uid", "kind", "from_year", "to_year", "note", "created_at",
		"updated_at",
	}},
	{"subject_family_children", []string{"family_uid", "child_uid"}, []string{
		"family_uid", "child_uid", "kind", "created_at",
	}},
	{"subjects", []string{"uid"}, []string{
		"uid", "slug", "name", "type", "favorite", "private", "notes", "cover_photo_uid", "created_at",
		"updated_at", "birth_year", "death_year", "nickname",
	}},
	{"upload_link_photos", []string{"link_uid", "photo_uid"}, []string{
		"link_uid", "photo_uid", "outcome", "uploader_name", "uploaded_by", "session_hash", "created_at",
	}},
	{"upload_links", []string{"uid"}, []string{
		"uid", "code_hash", "title", "note", "created_by", "created_at", "expires_at", "revoked_at",
		"upload_count", "last_used_at", "code",
	}},
	{"user_favorites", []string{"user_uid", "photo_uid"}, []string{"user_uid", "photo_uid", "added_at"}},
	{"user_ratings", []string{"user_uid", "photo_uid"}, []string{
		"user_uid", "photo_uid", "rating", "flag", "updated_at",
	}},
	{"users", []string{"uid"}, []string{
		"uid", "username", "display_name", "email", "password_hash", "disabled", "created_at",
		"updated_at", "last_login_at", "note", "last_seen_at", "visit_reference_at", "subject_uid",
		"approved_at", "welcome_seen_at", "task_digest_at",
	}},
}

// hashedTypes are the column types digested through md5 of their text form
// rather than their full text: they are large (a 1152-dimension vector, an
// exiftool dump), and the per-row md5 changes exactly when the value does.
var hashedTypes = map[string]bool{
	"jsonb": true, "json": true, "halfvec": true, "vector": true, "bytea": true, "tsvector": true,
}

// textTypes are the column types an ORDER BY sorts under COLLATE "C", so the
// digest does not depend on the database's locale.
var textTypes = map[string]bool{"text": true, "varchar": true, "bpchar": true}
