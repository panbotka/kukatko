package photos

// TakenAtBeforeUnknownAssignment returns the `taken_at_before_unknown = …` SET
// clause that keeps the preserved capture date correct, given the SQL expression
// the same UPDATE assigns to taken_at.
//
// It is the one place the rule lives. Both write paths that clear a date — the
// single-photo PATCH through updateMetadataRow and the bulk clear_taken_at
// operation in internal/bulk — build their own statement, and duplicating a
// three-branch CASE across them is how the two would drift apart. See migration
// 0066 for why the column exists.
//
// The branches, in order:
//
//   - the statement states a date → nothing is set aside any more, so the
//     preserved value is dropped;
//   - the date goes from a value to NULL → that outgoing value is preserved;
//   - the date was already NULL → the column keeps whatever it holds, so a clear
//     after a clear cannot overwrite a real preserved date with NULL, and
//     clearing a photo that never had a date changes nothing.
//
// takenAtExpr must be an expression under the caller's control — a positional
// placeholder ("$6") or the literal "NULL" — never user input; it is cast to
// timestamptz so a bare NULL and an untyped parameter both resolve.
func TakenAtBeforeUnknownAssignment(takenAtExpr string) string {
	return "taken_at_before_unknown = CASE" +
		" WHEN (" + takenAtExpr + ")::timestamptz IS NOT NULL THEN NULL" +
		" WHEN photos.taken_at IS NOT NULL THEN photos.taken_at" +
		" ELSE photos.taken_at_before_unknown END"
}

// TakenAtSourceUnknown is the taken_at_source stamped when a date is cleared:
// the photo's capture time is not merely absent, somebody declared it unknown.
// The rest of the vocabulary ("exif", "manual", "filename") lives with the
// extractor that writes it (internal/exif); this one value is here because both
// edit paths stamp it.
const TakenAtSourceUnknown = "unknown"
