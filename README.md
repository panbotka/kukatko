# Kukátko

**A self-hosted photo and video library for a personal or family archive** — one binary, one
PostgreSQL database, and your originals on your own disk. Kukátko is a replacement for
[PhotoPrism](https://photoprism.app/) that also brings over the "smart" parts of
[photo-sorter](https://github.com/kozaktomas/photo-sorter): semantic search, similar photos, and
recognition of the people in your pictures.

*Kukátko* is Czech for a peephole — the little lens you look through.

<!--
  SCREENSHOTS ARE MISSING and should be added by a human.
  Two would be enough: the library grid and a photo detail.
  Do NOT reuse the images in .shots/ — several of them (01-library, 03-albums, 07-map,
  12-duplicates) are frames of the live family library and show identifiable real people.
  Shoot them against a throwaway instance holding photos that are safe to publish.
-->

## Who it's for

You have twenty thousand photos, they span twenty years, and you have long since stopped being
able to find anything in them. You would like to ask for "the sunset from that trip" or for every
photo grandma is in — and you would rather not hand the family archive to a cloud service to get
that. You have a machine at home (a Raspberry Pi will do), you are not afraid of a PostgreSQL
connection string, and you want the originals to stay files on a disk you own.

That is who Kukátko is written for.

It is **not** for you if you are looking for:

- **a sharing service** — public share links are not a priority and are not implemented;
  Kukátko is a library you and the people with accounts on it browse, not a way to publish;
- **a photo-book tool** — deliberately out of scope (photo-sorter did it, Kukátko does not);
- **a product** — see [Status](#status) below.

## What makes it different

- **One executable.** A single static Go binary (`CGO_ENABLED=0`) with the whole React frontend
  embedded in it. No app server, no separate frontend deployment, no Node.js at runtime.
- **Postgres is the only datastore** — and that includes the vectors. CLIP image embeddings and
  face embeddings live in `pgvector` columns (`halfvec` + HNSW cosine), so a similarity search is
  an ordinary SQL query. There is no second database to run, back up, or keep in sync.
- **Search that understands the picture and the query.** Full-text, semantic (CLIP), and a hybrid
  of both; plus a query language that mixes free text with `key:value` filters in one box —
  `dovolená camera:"Canon EOS R6" iso:100-400 faces:2`, `label:cat|dog`, `taken:2024-05`,
  `near:<uid>`. The same language works in the library's own search field, which offers the same
  `?` cheat sheet, points out a filter it did not understand, and flags the facet pickers the query
  has already set. The time axis is one control, not two: a **period filter** offering the decades
  the library holds, each expandable to its years, with exact dates underneath — so "the sixties",
  "before 1950" and "summer 2019" are all a click or two away, and the picker shows the period a
  `year:` in the search box has already set instead of contradicting it. Faces are detected,
  clustered, and matched to named people; nothing is ever assigned automatically — the machine
  narrows the list, a human confirms. And because a catalogue of people goes wrong in exactly two
  ways, a person's page carries the repair for each: **merge** the same person filed twice into one
  record (everything they carried moves over, nothing is deduplicated away), and **move** the photos
  in their gallery that turn out to be somebody else to whoever they really are.
- **A person's gallery reads as a life.** Give somebody a birth (and, if it applies, a death) year —
  both optional, and a year is all anybody usually knows — and their page becomes a timeline: the
  span beside their name ("1923–1998"), roughly how old they were on every dated photograph
  ("~23 let", never a negative or absurd number), and a jump straight to the decade you meant. In an
  archive spanning 1905–2026 that is the difference between a pile of pictures and a biography.
- **It tells you what you missed.** A family library is shared, so somebody else's evening of
  uploading is invisible to whoever opens the app next. Come back after a while and the library
  greets you with what happened while you were away: how many photos arrived (one click to see
  them), which albums were started, who was newly named, how many comments were written. A "visit"
  is measured server-side and forgiving — reloading the page or wandering off to an album and back
  does not reset it, only a real absence does — so the summary stays put while you read it, closes
  for good when you close it, and never greets a brand-new account with its own library as news.
- **Tidying up is a game you can play one keypress at a time.** Everything the machine guesses at
  ends up as a single yes/no question on a full screen — is this Tomáš? does this label fit? was
  this taken in Brno? is this the same photo twice? is the person tagged here really her? — with
  Ano · Ne · Nevím under it and the arrow keys doing the work. The last three of those check things
  the machine did not merely guess but **acted on**: a location inferred from neighbouring photos, a
  pair the duplicate detector linked, a face already filed under somebody. Those are the mistakes
  that otherwise sit unnoticed, because nothing else lists them as questions. Every answer goes
  through the same write paths the ordinary pages use, so it is audited and undoable — and the game
  never merges, never deletes and never invalidates anything on its own. A leaderboard counts who
  has answered the most, because a shared library is tidied by more than one person.
- **A library an AI agent can use.** Kukátko can expose itself as an **MCP server**, so an agent
  searches and organizes the library directly ("find all of grandma's photos from the sixties and
  put them in an album"). Off by default, authenticated with an ordinary API token, bound by the
  same RBAC, audited in the same transaction — and **nothing destructive is exposed**.
- **Built for small hardware that is sometimes offline.** It runs on a Raspberry Pi and delegates
  embedding computation to a stronger machine over HTTP. That machine can be powered off: the jobs
  wait in a durable Postgres-backed queue, and upload, browsing, and search keep working without
  it. Restarting the server loses no work.
- **The catalogue survives losing the database.** Every photo's metadata and curation — title,
  description, dates, people, albums, labels — is also written to a versioned YAML sidecar next to
  the original. Lose the DB and the meaning of your library is still on the disk.
- **Czech first, English second.** The UI ships bilingual (`cs` default), because it was written
  for a family that speaks Czech. Even the English album names the import left behind (`January
  2026`, a country) read in Czech — at display time only, without touching what is stored.

- **A phone is the first screen, not the leftover one.** Photos are browsed mostly on phones, so
  that is where the chrome is cheapest: the library's header there is the search field and the
  Filters button, with everything else — sort, tile density, the pickers, the view actions — folded
  into a drawer that has room for them, and the first photo starts 150 px down an 852 px screen
  instead of 371. The **timeline** — a date rail that jumps twenty thousand photos to 1965 in one
  tap — is on the phone too, as a narrow strip of years along the right edge that fades to a hint
  when you are not using it.

Around that sit the ordinary things a library needs and Kukátko has: albums — each covered by a
collage of four of its photos, so two albums drawn from the same afternoon don't look alike, each
saying underneath what it actually is, and each read from its oldest end or turned round with one
switch; an album spanning more than a couple of years gets the same timeline rail the library has — and
labels, videos (range streaming, live photos), a map that says out loud how much of the library
carries a location at all, browsing by place with a preview photo on every row, a slideshow, per-user
favourites and ratings, bulk editing, multi-upload from a phone, duplicate detection, a trash with
retention, accounts with four roles, a durable audit trail, S3 backup with a restore runbook, and
import from any folder on disk — a Google Takeout export included, sidecars and all. Every view
lives in the URL and every browser tab is named after what it shows (`Svatba 1965 · Kukátko`), so
Back, a second tab, a bookmark and the history list all work the way the rest of the web does.

## Status

Kukátko is **deployed and in daily use** against a real family library of roughly twenty thousand
photos. The migration off PhotoPrism and photo-sorter finished in August 2026; the importers that
carried it have been removed, and Kukátko now stands on its own.

It is also, unambiguously, **a personal project and not a product**. There is no support, no
release cadence you can rely on, and no promise that the next version will not change something
you depend on. It is published because it may be useful to someone with the same problem, not
because it is finished. Issues and patches are welcome; a reply is not guaranteed.

## Quick start

**What you need**

- **PostgreSQL 17** with the `vector` (pgvector) and `unaccent` extensions available — Kukátko
  creates them itself on the first migration.
- Optional, and worth having: `exiftool`, `ffmpeg`, and `heif-convert` (libheif) on the `PATH` —
  for EXIF, video posters and playback, HEIC, and RAW previews. Without them those formats
  degrade; everything else works.
- Optional: an inference sidecar (CLIP + InsightFace behind a small HTTP API) for semantic search
  and face recognition. It may be offline, and it may live on another machine.

**Get it**

A `.deb` (amd64 and arm64) is attached to every
[release](https://github.com/panbotka/kukatko/releases), and a container image is published to
`ghcr.io/panbotka/kukatko`. Or build it from source (Go 1.26+, Node.js 22+):

```bash
make build     # frontend build + a static binary in bin/kukatko
```

**Run it**

```bash
export KUKATKO_DATABASE_URL="postgres://kukatko:…@localhost:5432/kukatko"
export KUKATKO_AUTH_BOOTSTRAP_ADMIN_USERNAME="admin"
export KUKATKO_AUTH_BOOTSTRAP_ADMIN_PASSWORD="…"   # only used while the users table is empty

./bin/kukatko serve      # applies migrations, creates the first account, listens on :8080
```

`database.url` is the only required setting; everything else has a default. Configuration is a
YAML file with environment overrides (`KUKATKO_` prefix, dots become underscores) — every key is
documented in [`config.example.yaml`](config.example.yaml).

Filling the library, backing it up, restoring it, and driving a running instance from the command
line (`kukatko import`, `backup`, `restore`, `maintenance`, `ctl`) are all covered in
[`docs/OPERATIONS.md`](docs/OPERATIONS.md). For a development loop, read
[`docs/DEVELOPMENT.md`](docs/DEVELOPMENT.md).

## Documentation

| I want to… | Read |
| --- | --- |
| understand the design, the data model, and where the project is going | [`docs/ARCHITECTURE.md`](docs/ARCHITECTURE.md) |
| build, run, and test it locally | [`docs/DEVELOPMENT.md`](docs/DEVELOPMENT.md) |
| operate it — CLI, configuration keys, `make`, CI, packaging | [`docs/OPERATIONS.md`](docs/OPERATIONS.md) |
| call the HTTP API, including the search query grammar | [`docs/API.md`](docs/API.md) |
| point an AI agent at the library | [`docs/MCP.md`](docs/MCP.md) |
| find my way around the Go packages | [`docs/PACKAGES.md`](docs/PACKAGES.md) |
| find my way around the frontend | [`docs/FRONTEND.md`](docs/FRONTEND.md) |
| make it fast on small hardware | [`docs/PERF.md`](docs/PERF.md) |
| rebuild an instance from a backup | [`docs/RESTORE.md`](docs/RESTORE.md) |
| know what did and did not come across from PhotoPrism and photo-sorter | [`docs/MIGRATION_AUDIT.md`](docs/MIGRATION_AUDIT.md), [`docs/MIGRATION_PLAN.md`](docs/MIGRATION_PLAN.md), [`docs/READINESS_AUDIT.md`](docs/READINESS_AUDIT.md) |
| read the security review | [`docs/SECURITY_AUDIT.md`](docs/SECURITY_AUDIT.md) |
| read the usability review | [`docs/UX_AUDIT.md`](docs/UX_AUDIT.md), [`docs/UX_RESEARCH.md`](docs/UX_RESEARCH.md) |
| write code for it — conventions and hard rules | [`CLAUDE.md`](CLAUDE.md) |

## License

[MIT](LICENSE) © 2026 Pan Botka.
