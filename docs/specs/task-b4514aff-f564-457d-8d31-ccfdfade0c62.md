# Recognition sweep — "who else is in the library, unnamed?"

The `/faces` page answers the question for **one** person you pick. The sweep answers it for **everyone at
once**: run the candidate search across all named subjects at a high confidence, and present the result
grouped by person — a work list that visibly shrinks as you clear it.

**Confirm first** that `POST /api/v1/subjects/{uid}/candidates` exists (from the person-search task) and that
persisted rejections exist (`internal/feedback`). Both are prerequisites — this task composes them, it does
not re-implement them.

## Backend

Photo-sorter implements this as a **client-side fan-out**: the browser fires one request per subject, three
at a time. Do not copy that. It makes the browser the scheduler, it re-embeds nothing but re-queries
everything, it cannot be cached, and it falls apart on a slow connection.

Add a **server-side sweep** endpoint that calls the existing per-subject candidate service internally:

`GET /api/v1/faces/sweep?confidence=<percent-or-distance>&limit=<per-person>` (RequireWrite)

- Iterates named subjects that have at least one face, running the per-subject search with a **high**
  confidence (a tight distance) — the point of the sweep is *confident* matches, not exploration.
- Concurrency is **bounded server-side** (a small worker pool — this box is RAM-constrained; do not fan out
  over hundreds of subjects at once).
- A subject with **zero actionable candidates is omitted entirely** from the response. The result is a work
  list, not a report.
- Returns, per person: the subject, and its candidate faces in the same shape the per-subject endpoint
  returns (photo, bbox, distance, match count, action). Plus a global summary: people scanned, people with
  matches, total actionable, total already done.
- **Never auto-accepts.** Confidence narrows the list; every write still needs a human. Say this in the docs
  so a future iteration does not "helpfully" add auto-assign.
- The sweep can be slow. Stream the result if the existing HTTP conventions make that natural; otherwise cap
  it sensibly and make the cap visible in the response rather than silently truncating.

## Frontend

New route **`/recognition`** (editor-only), linked from the people section of the nav.

- Config panel: a **confidence slider in percent**, range 50–95, step 1, **default 75** (a tight default — this
  page is for the easy wins), plus a per-person limit. A Scan button.
- Live progress while scanning: a bar with `current/total` and the name of the person being scanned.
  Cancellable. If the backend streams, results appear person by person as they arrive rather than all at the
  end — the wait is the worst part of this feature, so do not make the user stare at a spinner.
- One card per person: header = name + actionable count + **"Potvrdit vše (n)"**; body = the same
  bbox-annotated candidate grid the `/faces` page uses. **Reuse that component — do not fork it.** If it is
  not currently reusable, extract it as part of this task.
- Per card ✓ = confirm (existing assign endpoint), ✗ = **persisted rejection** (`POST /api/v1/feedback/face-rejections`).
- **When a person's last candidate is cleared, the whole person card disappears.** The list shrinking is the
  reward loop; without it the page feels like it never ends.
- Keyboard: the same shortcuts as the `/faces` page (`y`/`Enter` confirm, `n` reject, arrows move), registered
  in the `?` help overlay. Reuse, do not duplicate.
- Global stats: actionable / already done / people with matches. A clean empty state after a scan with no
  hits ("všechny obličeje jsou přiřazené").

## i18n

Every string into **both** `web/src/i18n/locales/cs/common.json` and `.../en/common.json`.

## Verification

- Backend integration test: a library with three subjects where only one has an unnamed match → only that
  subject appears in the response; a rejected candidate never appears; the concurrency bound is respected;
  a subject with no faces is skipped without erroring.
- Vitest: progress renders during the scan; a person card disappears when its last candidate is cleared;
  "Confirm all" walks one person's list; cancel stops the scan.
- `make check` must pass.
- Document: `docs/API.md`, `docs/FRONTEND.md`, `docs/PACKAGES.md` if a package appears, `README.md`
  (user-visible feature), and any new config key in `docs/OPERATIONS.md` + `config.example.yaml`.

## Out of scope

- Auto-accepting high-confidence matches without a human.
- Scheduling the sweep as a background job — it is user-triggered.
- Clustering (`internal/cluster`) — it stays as it is. The sweep is person-first and covers the singleton
  unnamed faces clustering structurally cannot reach; the two are complementary.