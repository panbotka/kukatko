# Kukátko

[![release](https://img.shields.io/github/v/release/panbotka/kukatko)](https://github.com/panbotka/kukatko/releases)
[![license](https://img.shields.io/github/license/panbotka/kukatko)](LICENSE)
![go](https://img.shields.io/badge/Go-1.26-00ADD8)
![postgres](https://img.shields.io/badge/PostgreSQL-17%20%2B%20pgvector-336791)

**A self-hosted photo and video library for a personal or family archive.** One static binary, one
PostgreSQL database, and your originals as ordinary files on a disk you own. Ask it for "the sunset
from that trip" and it finds the picture even though nobody ever typed a word about it; ask it for
every photo grandma is in and it knows her face. It runs on a Raspberry Pi.

*Kukátko* is Czech for a peephole — the little lens you look through.

<!--
  SCREENSHOTS ARE MISSING and should be added by a human.
  Two would be enough: the library grid and a photo detail.
  Do NOT reuse the images in .shots/ — they are frames of the live family
  library and show identifiable real people. Shoot them against a throwaway
  instance holding photos that are safe to publish.
-->

## Who it's for

You have twenty thousand photos, they span twenty years, and you long ago stopped being able to find
anything in them. You would rather not hand the family archive to a cloud service to fix that. You
have a machine at home, you are not afraid of a PostgreSQL connection string, and you want the
originals to stay files on a disk you own.

It is **not** for you if you want a sharing service (public share links are not implemented), a
photo-book tool (deliberately out of scope), or a product with support and a release cadence — see
[Status](#status).

## Features

### Finding things

- **Three search modes in one box** — full-text, semantic (CLIP image/text embeddings), or a hybrid
  of both, which is the default. Semantic search finds a picture by what it *looks like*, so an
  untitled, untagged, undescribed photo is still reachable.
- **A query language** that mixes free text with `key:value` filters in the same field:
  `dovolená camera:"Canon EOS R6" iso:100-400 faces:2`, `label:cat|dog`, `taken:2024-05`,
  `person:"Anna Nováková" year:1985`, `near:<uid>` for "more like this one". Some 40 keys — people,
  albums, labels, country/city, `geo`, camera, lens, `iso`, `f`, `mm`, `mp`, media type, codec,
  orientation, rating, favourite. `person:`, `label:` and `album:` autocomplete the **actual names in
  your library**, diacritics-insensitively and quoted correctly; a `?` cheat sheet is one keypress
  away, and a filter the parser did not understand is pointed out instead of silently ignored.
- **A time axis shaped like memory** — a period filter offering the decades your library actually
  holds, each expanding to its years and then to exact dates, plus a timeline rail that jumps twenty
  thousand photos to 1965 in one tap (on the phone too).
- **Saved searches** ("smart albums") and a per-account search history, so a query composed on the
  laptop is one keystroke away on the phone. Every view lives in the URL: Back, a second tab, and a
  bookmark all behave the way the rest of the web does.

### People

- Faces are detected, clustered, and matched against the people you have named — and **nothing is
  ever assigned automatically**. The machine narrows the list down, a human confirms.
- Tools for the parts that are actually hard: candidates for one person among the untagged faces,
  auto-clusters of unknown faces, outliers inside a person's collection (the face that probably isn't
  them), and a recognition sweep across the whole library.
- **A person's page reads as a life.** Give somebody a birth (and, if it applies, a death) year — a
  year is all anybody usually knows — and their page becomes a timeline: the span beside their name
  ("1923–1998"), roughly how old they were in every dated photograph, and a jump straight to the
  decade you meant.
- A catalogue of people goes wrong in exactly two ways and both have a repair: **merge** the same
  person filed twice into one record, and **move** the photos that turn out to be somebody else to
  whoever they really are.

### Tidying up, as a game

Everything the machine guesses at ends up as a single yes/no question on a full screen — is this
Tomáš? does this label fit? was this taken in Brno? is this the same photo twice? is the person
tagged here really her? — with **Ano · Ne · Nevím** (yes · no · don't know) underneath and the arrow
keys doing the work; on a phone the three answers are a swipe. Questions come in rounds of ten,
deliberately mixed by person, kind, difficulty and era, so a session never feels like an
interrogation about one person. Every answer goes through the same write paths the ordinary pages
use, so it is audited and undoable — and the game never merges, deletes or invalidates anything on
its own. A leaderboard counts who has answered the most and how many days in a row, because a shared
library is tidied by more than one person.

### The everyday library

Albums (each covered by a collage of four of its photos, so two albums from the same afternoon don't
look alike) · labels · per-user favourites and ratings · comments · bulk editing, including re-dating
a whole shelf of scans at the grain you actually know ("1974", "the seventies") · duplicate detection
with a side-by-side compare and a merge that keeps every album, label and person · RAW+JPEG and
edited-variant stacks (grouped, never merged) · non-destructive crop/rotate/brightness/contrast ·
a trash with retention · a slideshow · and a statistics page that answers what the library really
looks like — photos per year across its whole span, arrivals per month, which cameras took most of
it, what it costs in bytes, and how far along the processing is.

### Places and video

- A **map** with clustering that says out loud how much of the library carries a location at all,
  browsing by place (country → city, with a preview photo on every row), and reverse geocoding. A
  missing location can be estimated from photos taken near it in time — and only when those
  neighbours cluster tightly, always marked as an estimate.
- **Video** with range streaming, poster frames, live photos, and a player with playback speed,
  ±10 s skips, and frame previews under the cursor as you scrub.

### On a phone

The phone is the first screen, not the leftover one: the library's header there is the search field
and a Filters button, with sort, density and the pickers folded into a drawer. **"Add to Home
Screen"** installs it with its own icon and no browser chrome; a service worker keeps the app shell
on the device so it opens instantly and says plainly that you are offline instead of failing
blankly — it caches nothing else, so nothing you see is ever stale. On **Android**, Kukátko is a
share target: pick shots in the gallery, tap Share, and the upload page opens with them queued.

### It tells you what you missed

A family library is shared, so somebody else's evening of uploading is invisible to whoever opens the
app next. Come back after a while and the library greets you with what happened: how many photos
arrived (one click to see them), which albums were started, who was newly named, how many comments
were written.

### Built for small hardware that is sometimes offline

- **One executable.** A single static Go binary (`CGO_ENABLED=0`) with the whole React frontend
  embedded in it. No app server, no separate frontend deployment, no Node.js at runtime.
- **Postgres is the only datastore — vectors included.** CLIP image embeddings and face embeddings
  live in `pgvector` columns (`halfvec` + HNSW cosine), so a similarity search is an ordinary SQL
  query. There is no second database to run, back up, or keep in sync.
- **The GPU machine may be switched off.** Embedding and face detection are delegated over HTTP to a
  stronger box; while it is down the jobs wait in a durable Postgres-backed queue, upload, browsing
  and full-text search keep working, and semantic search degrades to full-text and says so.
  Restarting the server loses no work.

### Yours to keep

- **The catalogue survives losing the database.** Every photo's metadata and curation — title,
  description, dates, people, albums, labels — is also written to a versioned YAML sidecar next to
  the original. Lose the DB and the meaning of your library is still on the disk.
- Originals on **local disk** or in an **S3-compatible bucket** (Cloudflare R2, with short-lived
  signed URLs), plus **S3 backup** — `pg_dump` and originals with retention — and a restore runbook
  that has been used.
- Four roles (viewer < editor < admin < maintainer), a durable **audit trail** written in the same
  transaction as the change it records, personal **API tokens**, Prometheus metrics, and
  `kukatko ctl` — a kubectl-style CLI against your own instance.
- **A library an AI agent can use:** Kukátko can expose itself as an **MCP server**, so an agent
  searches and organizes the library directly ("find grandma's photos from the sixties and put them
  in an album"). Off by default, authenticated with an ordinary API token, bound by the same RBAC —
  and nothing destructive is exposed.
- **Czech first, English second.** The UI ships bilingual (`cs` default, `en`).

## What you need

- **A Linux host**, amd64 or arm64. A Raspberry Pi 5 is a perfectly reasonable target and is what
  this is developed against.
- **PostgreSQL 17** with the `vector` (pgvector) and `unaccent` extensions available — Kukátko
  installs both itself on the first start, so an empty database is all you have to create. Nothing
  else: no Redis, no message broker, no Elasticsearch.
- **Disk** for the originals and a cache directory for thumbnails.
- **Optional, and worth having:** `exiftool`, `ffmpeg`/`ffprobe` and `heif-convert` (libheif) on the
  `PATH`, for EXIF, video posters and playback, HEIC, and RAW previews. Without them those formats
  degrade; everything else works. `vipsthumbnail` (libvips) is optional too and only makes
  thumbnailing faster.
- **Optional: an embeddings service** for semantic search and face recognition — see
  [below](#semantic-search-and-faces-optional). Everything except those two features works without
  it.

## Getting it running

### Docker Compose

The fastest path from nothing to a library. The published image is currently **linux/amd64** only —
on a Raspberry Pi or another arm64 host, use the `.deb` or build from source below.

```yaml
# docker-compose.yml
services:
  db:
    image: pgvector/pgvector:pg17
    environment:
      POSTGRES_USER: kukatko
      POSTGRES_PASSWORD: change-me
      POSTGRES_DB: kukatko
    volumes:
      - db:/var/lib/postgresql/data
    # Kukátko dials the database once at startup and exits if it is not there
    # yet, so wait for Postgres to actually accept connections, not merely to
    # have been started.
    healthcheck:
      test: ["CMD-SHELL", "pg_isready -U kukatko -d kukatko"]
      interval: 5s
      timeout: 5s
      retries: 10
    restart: unless-stopped

  kukatko:
    image: ghcr.io/panbotka/kukatko:latest
    depends_on:
      db:
        condition: service_healthy
    restart: unless-stopped
    ports:
      - "8080:8080"
    environment:
      KUKATKO_DATABASE_URL: postgres://kukatko:change-me@db:5432/kukatko?sslmode=disable
      # `openssl rand -hex 32` — signs session cookies.
      KUKATKO_WEB_SESSION_SECRET: change-me-to-a-long-random-string
      # Only used while the users table is empty; drop them after the first start.
      KUKATKO_AUTH_BOOTSTRAP_ADMIN_USERNAME: admin
      KUKATKO_AUTH_BOOTSTRAP_ADMIN_PASSWORD: change-me
      # Optional; the service may be offline or absent entirely.
      # KUKATKO_EMBEDDING_URL: http://embeddings:8000
    volumes:
      - originals:/var/lib/kukatko/originals
      - cache:/var/lib/kukatko/cache

volumes:
  db:
  originals:
  cache:
```

```bash
docker compose up -d      # migrations run on start; the admin account is created
```

Then open <http://localhost:8080> and sign in. Behind a TLS-terminating reverse proxy, add
`KUKATKO_WEB_SECURE_COOKIES: "true"`.

### Debian / Ubuntu package

A `.deb` for **amd64 and arm64** is attached to every
[release](https://github.com/panbotka/kukatko/releases). It brings the media tools as dependencies,
creates an unprivileged `kukatko` user, and installs a systemd unit:

```bash
sudo dpkg -i kukatko_*_linux_arm64.deb
sudo editor /etc/kukatko/kukatko.env     # at minimum KUKATKO_DATABASE_URL + KUKATKO_WEB_SESSION_SECRET
sudo systemctl start kukatko             # http://<host>:8080
```

### From source

Go 1.26+ and Node.js 22+:

```bash
make build               # frontend build + a static binary in bin/kukatko

export KUKATKO_DATABASE_URL="postgres://kukatko:…@localhost:5432/kukatko"
export KUKATKO_AUTH_BOOTSTRAP_ADMIN_USERNAME="admin"
export KUKATKO_AUTH_BOOTSTRAP_ADMIN_PASSWORD="…"   # only while the users table is empty

./bin/kukatko serve      # applies migrations, creates the first account, listens on :8080
```

### Configuration

`database.url` is the **only required setting**; everything else has a default. Configuration is an
optional YAML file with environment overrides — prefix `KUKATKO_`, dots become underscores
(`web.port` → `KUKATKO_WEB_PORT`), and the environment always wins. Every key is documented in
[`config.example.yaml`](config.example.yaml); a container-shaped subset is in
[`.env.example`](.env.example).

### Filling the library

Upload from the browser (or from an Android share sheet), or point the CLI at a folder:

```bash
kukatko import dir /mnt/photos/2019
```

It walks the directory recursively, uploads through the same pipeline as a browser upload, and
**never touches the source**. Identity is the SHA256 of the content, so re-running it is always safe:
anything already in the library is reported as a duplicate and nothing is written. It is resumable,
and it reads **Google Takeout `.json`** and Apple `.xmp` sidecars — which is the difference between
keeping and losing the capture dates, captions and GPS of a Google Photos export.

## Semantic search and faces (optional)

The two "smart" features need an inference service that Kukátko does **not** ship: a small HTTP
sidecar wrapping CLIP (image + text, 768-dim) and InsightFace (512-dim), pointed at by
`KUKATKO_EMBEDDING_URL`. It is deliberately a separate process so it can live on a machine with a GPU
— one that is allowed to be powered off, since the queue simply waits.

Anything answering these three endpoints will do:

- `POST /embed/image` — multipart, field `file` → `{ "dim": 768, "embedding": [...] }`
- `POST /embed/text` — JSON `{ "text": "..." }` → the same shape, in the same vector space
- `POST /embed/face` — multipart, field `file` →
  `{ "faces_count": N, "faces": [{ "dim": 512, "embedding": [...], "bbox": [x1,y1,x2,y2], "det_score": 0.9 }] }`

Without it, upload, browsing, full-text search, albums, labels, places, video and everything else
work as usual; `image_embed`/`face_detect` jobs queue up and drain whenever the service appears. The
exact contract is in [`docs/ARCHITECTURE.md` §6.1](docs/ARCHITECTURE.md).

**Maps** are the other optional integration: set `MAPY_API_KEY` (a [mapy.com](https://developer.mapy.com/)
REST key) and the map view lights up. The key stays server-side — tiles and geocoding are proxied,
never handed to the browser.

## Status

Kukátko is **deployed and in daily use** against a real family library of roughly twenty thousand
photos, and it is also, unambiguously, **a personal project and not a product**. There is no support,
no release cadence you can rely on, and no promise that the next version will not change something
you depend on. It is published because it may be useful to someone with the same problem, not because
it is finished. Issues and patches are welcome; a reply is not guaranteed.

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
| read the security review | [`docs/SECURITY_AUDIT.md`](docs/SECURITY_AUDIT.md) |
| read the usability review | [`docs/UX_AUDIT.md`](docs/UX_AUDIT.md), [`docs/UX_RESEARCH.md`](docs/UX_RESEARCH.md) |
| write code for it — conventions and hard rules | [`CLAUDE.md`](CLAUDE.md) |

## License

[MIT](LICENSE) © 2026 Pan Botka.
