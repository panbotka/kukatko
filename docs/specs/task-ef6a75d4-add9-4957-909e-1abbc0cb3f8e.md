# M6 — Duplicates review & cleanup

Add a tool to find and review near-duplicate photos and clean them up in bulk — beyond the upload-
time warning.

## Context
Read `docs/ARCHITECTURE.md` §5 (photo_phashes), §6 (embeddings). pHash/dHash and embeddings already
exist; upload warns on near-dups but there is no review surface. Mirrors photo-sorter's duplicate
finding. Depends on photos, phashes, embeddings, bulk/archive APIs.

## Requirements
- Backend: `GET /api/v1/duplicates` that returns **groups** of likely duplicates using pHash
  Hamming distance (config `duplicate.phash_max_diff`) and/or embedding cosine distance
  (config `duplicate.embedding_max_dist`). Each group lists members with enough info to compare
  (thumb, dimensions, size, taken_at, file size) and a suggested "keeper" (e.g. largest/highest
  resolution). Paginated; efficient (avoid O(n^2) scans — use phash buckets / vector neighbors).
- Frontend **Duplicates page** (editor/admin): show groups side by side; let the user pick which to
  keep and **archive/delete the rest** (reuse bulk/archive APIs), or dismiss a group as
  "not a duplicate". Progress + result feedback.
- Safe: never auto-deletes; user confirms. i18n (cs/en), responsive.

## Quality gate (mandatory)
- Use the **golang-developer** skill for backend; `make check` MUST pass (incl. frontend lint/test).
- Integration tests (test DB): planted near-duplicates (close pHash and/or embeddings) are grouped;
  distinct photos are not; suggested keeper logic; pagination. Vitest: duplicates UI renders groups,
  keep/archive action calls bulk API, dismiss removes the group from view. Mock API.