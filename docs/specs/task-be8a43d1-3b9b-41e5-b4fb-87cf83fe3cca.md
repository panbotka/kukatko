## Context
Round 3 review of the multi-tenancy programme (2026-10-15, main 996ae051). Reports: <path>/multitenancy-review-3/round3/walk.md §W4, W6, W7, W8, W9/W10; <path>/multitenancy-review-3/round3/isolation.md §Nice N1, N3, N4, N5, N6, N7; <path>/multitenancy-review-3/round3/fixes.md §Findings 14 (bullets not covered by other tasks: facematch marker/photo check, disable vs FK-insert deadlock → one 500, approval mail with no addressee, resolver confirms a private photo exists within the caller's own project). Each item there has its evidence (reproduced or file:line). Verify each still holds; skip and report those already fixed.
Items:
- /account "Role" shows the last-visited project's role without naming it (W4).
- Deactivated project: a member gets "Stránka nenalezena" vs `/projects` "Nedostupné"; deactivated while open → only "Štítky se nepodařilo načíst." (W6) — show the project-unavailable state.
- /account/activity: raw action codes, task.create row links to a photo uid, project column shows slugs not names (W7), cs + en.
- "Podobné fotky" lists archived photos (internal/photoapi/similar.go:96-137) (W8).
- /system "Kompletní statistiky" links to the last project's stats under instance-wide counts (W9).
- Members table "Poslední přihlášení" = last login anywhere → a project admin sees activity in other projects (W10): show last activity in this project instead.
- `/u/{code}` 410 revoked vs 404 random (N1) → identical.
- Job handlers fall back to the bootstrap project when the payload lacks project_uid (clusterjob.go:117, familyexportjob.go:132, notifypurgejob.go:100, namelessjob.go:381) (N3) → resolve from the row or fail loudly; keep pre-upgrade jobs working (they are drained at release — prove it or keep a logged fallback).
- notification photos inserted without a same-project check (notification/store.go:89-92) (N4).
- Frontend keys: last picked map location (lib/pickedLocation.ts:10) and the anonymous uploader name (pages/UploadLinkPage.tsx:51) keyed by project / cleared at sign-out; setCurrentProjectSlug not during render (ProjectProvider.tsx) (N5).
- instance /audit/mine lists actions in a project you were removed from (N6) — decide and document.
- facematch/apply.go:91,112: marker must belong to the route's photo (N7).
- the fixes.md bullets listed above.
## Requirements
Each item: a test that fails on the current code (vitest / unit / integration), the fix, docs routed per CLAUDE.md (API.md / FRONTEND.md / PACKAGES.md), i18n cs + en. Browser verification on box staging for UI items. If an item turns out larger than polish, implement the rest and report it in the commit message instead of half-doing it.
## Out of scope
Everything covered by the other "Multi-tenancy review III" tasks.