# Person Merge and Split

Let editors merge duplicate people and move misassigned faces out to another person. With 105 imported subjects, duplicate people exist and there is currently no way to fix them.

## Requirements

### Merge
- On a person's page (SubjectPage), an editor can choose "Merge into another person": a searchable person picker (excluding the current person), then a confirmation dialog showing both persons' face thumbnails and photo counts, with a clear warning that the action cannot be undone.
- Merging person A into keeper B moves everything to B: face markers, exemplars, feedback (confirmations/rejections), and any per-user subject favorites. A is then deleted. No orphan rows may remain.
- If both A and B had a marker on the same photo, the result must not corrupt the photo: either keep both markers (the existing duplicate-marker tooling will surface them) or dedup obviously identical ones — pick one behavior and test it.
- Conflicting feedback (e.g. "not this person" recorded for B on a photo where A was confirmed) must not break the merge; define precedence and test it.
- The whole merge runs in one transaction with an audit entry.

### Split
- On the person's page, an editor can select one or more of the person's faces/photos and "Move to another person": choose an existing person or type a new name to create one. Reuse the existing assignment write paths so the assignment state machine and audit behavior stay consistent.

### General
- Role: editor and above. Read-only viewers see neither action.
- i18n cs/en for all new UI texts.
- Integration tests for merge semantics (markers moved, favorites/feedback carried, same-photo conflict, no orphans); component tests for the pickers and confirmation flow.
- Update docs/API.md, docs/PACKAGES.md, and docs/FRONTEND.md per repo conventions.