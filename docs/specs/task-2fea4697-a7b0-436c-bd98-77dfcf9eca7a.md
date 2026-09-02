# Human Language on Admin Pages

Admin/maintainer pages (Maintenance, Import, Duplicates, System) and the all-roles /stats page speak developer jargon: embeddings, hashes, orphans, dead jobs, "box" (docs/UX_AUDIT.md backlog item on admin jargon). The /import page prints a run's last_error verbatim — the last raw-error leak in the app (its own audit backlog item).

## Requirements

- Replace jargon on these pages with plain human copy in Czech and English. The exact wording is yours, but a non-technical family member must be able to read every sentence (e.g. "dead jobs" becomes something like "úlohy, které se trvale nepovedly"). Keep precise technical identifiers available where a maintainer genuinely needs them, tucked behind an expandable detail.
- /import: a failed run shows a friendly one-line summary; the verbatim error moves behind an expandable "technical details" element.
- Presentation-only change: no behavior, API, or data changes.
- All copy via i18n (cs default + en).

## Implementation notes

- Read the two docs/UX_AUDIT.md backlog items first and follow their intent; mark them resolved there if the doc tracks status.
- Update docs/FRONTEND.md if components change shape.