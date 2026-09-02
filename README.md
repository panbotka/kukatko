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

- **Three search modes in one box** — full-text, semantic (SigLIP 2 image/text embeddings), or a hybrid
  of both, which is the default. Semantic search finds a picture by what it *looks like*, so an
  untitled, untagged, undescribed photo is still reachable.
- **A query language** that mixes free text with `key:value` filters in the same field:
  `dovolená camera:"Canon EOS R6" iso:100-400 faces:2`, `label:cat|dog`, `taken:2024-05`,
  `person:"Anna Nováková" year:1985`, `near:<uid>` for "more like this one". Some 40 keys — people,
  albums, labels, country/city, `geo`, `dated` (`dated:no` is the pile of photos with no date at all,
  whether nobody ever knew it or somebody said so), camera, lens, `iso`, `f`, `mm`, `mp`, media type,
  codec, orientation, rating, favourite. `person:`, `label:` and `album:` autocomplete the **actual names in
  your library**, diacritics-insensitively and quoted correctly; a `?` cheat sheet is one keypress
  away, and a filter the parser did not understand is pointed out instead of silently ignored.
- **The text in the picture is searchable too** — a street sign, a shop front, a race number, a
  scanned newspaper page. Every photo is run through a text recogniser and what it says becomes part
  of the search index, so typing *veselice* also finds the photo whose only claim to the word is
  painted on a board in the background. It ranks *below* a real title, never above one, and `text:` is
  there when you want to search the signs and nothing else. (Print, not handwriting — no engine here
  reads Czech cursive.)
- **A time axis shaped like memory** — a period filter offering the decades your library actually
  holds, each expanding to its years and then to exact dates, plus a timeline rail that jumps twenty
  thousand photos to 1965 in one tap (on the phone too).
- **Filter by who brought the photos in** — after a family event several people upload into the same
  album; the filter bar offers that album's contributors with their counts (and the imported photos as a
  group of their own), so one person's share is one click, ready to select and download. `uploader:me`,
  `uploader:none` and a plain `uploader:tomas` say the same thing in the search box, and a photo's
  "Uploaded by" line links straight to everything that person contributed.
- **Saved searches** ("smart albums") and a per-account search history, so a query composed on the
  laptop is one keystroke away on the phone. Every view lives in the URL: Back, a second tab, and a
  bookmark all behave the way the rest of the web does.

### People

- **The index of people is a picture of your family, not a phone book.** It opens on whoever the
  archive is fullest of, with a search that ignores case and diacritics (`nemcova` finds `Němcová`),
  a split between people, animals and everything else, and an ordering by photo count, by name or by
  who was named most recently — all of it in the URL, so Back works and a link carries the view. Each
  tile is the person's own face, cut to size by the server, so a hundred people cost about as much to
  load as a page of the library grid.
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
- **Your account can say which of those people is you.** Set it once, on your own account page (an
  administrator can set it for somebody when creating the account), and the library starts answering
  questions it could not before: a "My photos" entry that opens the pictures you are on, `person:me`
  in a search — so `person:me year:1998` works and can be saved as a smart album — a line in the
  "what's new" digest counting the new photographs *you* are in, and your face instead of a letter
  beside everything you have written. Several accounts may be the same person (a household login and
  a personal one both are), deleting a person from the library never harms an account, and the page
  where you set it says out loud that it puts your face on your comments.

### Tidying up, as a game

Everything the machine guesses at ends up as a single yes/no question on a full screen — is this
Tomáš? does this label fit? was this taken in Brno? is this the same photo twice? is the person
tagged here really her? — with **Ano · Ne · Nevím** (yes · no · don't know) underneath and the arrow
keys doing the work; on a phone the three answers are a swipe. Out of the box the game is about
people — which kinds it asks, and in what proportion, is a config setting rather than an accident of
how much the machine happened to guess at. Questions come in rounds of ten, deliberately mixed by
person, kind, difficulty and era, and never more than two in a row about the same person, so a
session never feels like an interrogation. "Don't know" is listened to: say it three times about one
person and the game stops asking *you* about them for a while, then tries once more on a face you
have never been shown — never on the ones you already gave up on, and never as a statement that it
is not them. Every answer goes through the same write paths the ordinary pages
use, so it is audited and undoable — and the game never merges, deletes or invalidates anything on
its own. A leaderboard counts who has answered the most and how many days in a row, because a shared
library is tidied by more than one person.

### The everyday library

- **The wall shows photographs, not squares.** The library, an album, a search result — every grid is
  justified: rows of photos at their own proportions, aligned to a common height and running edge to
  edge. A panorama is wide, a portrait is tall, and nothing is cropped down to a square to fit a
  cell. How many go across is yours to set (the `−`/`+` beside the filters), and the setting is read
  as "about this many landscape photos", so portraits sit fewer to a row and panoramas more.

Albums (each covered by a collage of four of its photos, so two albums from the same afternoon don't
look alike; a new one can be started straight from a selection — "these forty photos are Ostatky 2022" —
or from a single photo, without leaving for the Albums page. The index of them opens on the albums
somebody actually made and leaves the machine-made month folders, moments and places to their own
sections, each carrying its count; above them a name search that ignores case and diacritics — `pout`
finds `Pouť` — and a choice of ordering: newest, by name, by photo count. Empty albums stay out of the
way until a switch asks for them, and the whole view lives in the URL, so Back works and a link carries
exactly what you were looking at) · labels (a wrapping cloud of chips, each carrying one of its own
photos, its name and its count, opening on the labels the library is actually full of rather than on the
alphabet; the same folded name search — `dovolena` finds `Dovolená` — and the numbered families a village
archive grows, `Dum11`, `Dum12`, `Dum20`, …, folded into one chip you can open, so house numbers no longer
occupy the whole first screen) · per-user favourites and ratings ·
comments · bulk editing, including re-dating
a whole shelf of scans at the grain you actually know ("1974", "the seventies") — or declaring by the
handful that the date is simply unknown, which sets the wrong date aside rather than destroying it: the
photo's detail still shows what it used to claim and puts it back in one click, and the filter bar's
"Datum: bez data" gathers everything left undated · duplicate detection
with a side-by-side compare and a merge that keeps every album, label and person · RAW+JPEG and
edited-variant stacks (grouped, never merged) · non-destructive crop/brightness/contrast and a rotation
either way, which the whole library follows — the thumbnails are rebuilt from the turned photo, so the grid
shows it the way you turned it, and the face frames turn with it ·
a trash with retention · a slideshow that asks how you want to watch before it starts — the
transition, the speed and how long the whole thing will take, repeat, shuffle (a random order the
server deals, so it covers the library and not just the first page), and whether each photo says its
title, its date and its description over the picture, all of it changeable mid-show without losing
your place — and once it is running the controls get out of the way: they fade a few seconds after
the last mouse move, key or tap, taking the cursor with them, and come back the moment you ask
(a tap on the picture is how you ask on a phone) · and a statistics page that answers what the library really
looks like — photos per year across its whole span, arrivals per month, which cameras took most of
it, what it costs in bytes, and how far along the processing is.

A photo's detail says the same thing about **itself**: which of the background computations —
metadata, thumbnails, the semantic fingerprint, faces, text in the picture, the place, the metadata
sidecar — have already run, which are waiting or being worked on right now, and which failed and why.
So "why doesn't this photo come up in search?" has an answer instead of a shrug: the AI box is
offline and the job is queued. A step that ran and found nothing — no face, no text — says so, rather
than pretending to be a gap. A maintainer can run any missing step for that one photo on the spot.
Right under it, the photo states how it got here as one fact: who uploaded it and when, or simply
that it was imported.

### Places and video

- A **map** with clustering that says out loud how much of the library carries a location at all,
  browsing by place (country → city, with a preview photo on every row), and reverse geocoding. A
  missing location can be estimated from photos taken near it in time — and only when those
  neighbours cluster tightly, always marked as an estimate.
- **Video** with range streaming, poster frames, live photos, and a player with playback speed,
  ±10 s skips, and frame previews under the cursor as you scrub.

### On a phone

The phone is the first screen, not the leftover one: the library's header there is the search field
and a Filters button, with sort, density and the pickers folded into a drawer, and the map is the
map — its base-layer tabs, dates and archive switch fold away the same way, so seven tenths of the
screen is the map itself rather than the controls above it. **"Add to Home
Screen"** installs it with its own icon and no browser chrome; a service worker keeps the app shell
on the device so it opens instantly. It caches nothing else, so nothing you see is ever stale — and
it says exactly that: opening the app with no signal gets you a page explaining that the photos need
a connection, not a login form that then blames your password. On **Android**, Kukátko is a
share target: pick shots in the gallery, tap Share, and the upload page opens with them already
going up.

**Uploading is one thing on screen at a time, and it starts by itself.** An empty page asks one
question — which photos — and answering it *is* the start: there is no upload button to find. The
page then becomes the wait, with how far along the batch is pinned to the bottom edge next to the one
control that stage has, and the albums and labels for the whole batch right there, because choosing
where the photos belong is what there is to do while they go up. Picking an album halfway through, or
after everything has landed, works the same. At the end you get a sentence — "Nahráno 20 fotek,
přidáno do: Pouť 2026" — and one button into the library; **Nahrát další** goes back for the next
batch keeping the albums, since it is nearly always the same event. If files failed, that is what the
sentence leads with and retrying them is the button. The per-file list is still there with its retry,
its remove and its errors-only filter — folded away, and it unfolds itself the moment something
fails.

It works the other way round too: **somebody else uploaded the photos and you want yours in your own
phone**. Filter, select, tap Share, and the originals go to the phone's share sheet — iOS offers
"Save Images" into Apple Photos, Android offers Google Photos. A big selection is handed over in
batches of twenty (a phone has a finite amount of memory), each with its own tap, and a RAW file goes
as its largest JPEG preview, because that is what a phone library can actually show. On a desktop the
button is not there and the ZIP download beside it is the answer.

### It tells you what you missed

A family library is shared, so somebody else's evening of uploading is invisible to whoever opens the
app next. Come back after a while and the library greets you with what happened: how many photos
arrived (one click to see them), which albums were started, who was newly named, how many comments
were written.

### Built for small hardware that is sometimes offline

- **One executable.** A single static Go binary (`CGO_ENABLED=0`) with the whole React frontend
  embedded in it. No app server, no separate frontend deployment, no Node.js at runtime.
- **Postgres is the only datastore — vectors included.** Image embeddings and face embeddings
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
- **Sign in with a passkey.** Add the fingerprint reader on your phone or laptop, or a hardware
  key, and use it instead of typing a password — the secret never leaves the device, so there is
  nothing to phish and nothing to reuse. A key registered here works only here. Several may be
  added, each under a name you choose, and any of them may be removed again: the password never
  stops working, so removing the last one strands nobody. The server keeps only public keys.
  Needs the instance's own address configured (`auth.passkey.*`, which defaults off the address
  already set for mail); without it the feature is simply not offered.
- **People can ask for an account themselves.** Switch registration on in the settings, tell the
  family the shared secret, and anybody who knows it can register — the sign-in screen then offers a
  registration form (and offers it only while registration is open) — but nobody gets in until an
  administrator approves them. They are e-mailed that they are waiting, every administrator is
  e-mailed that somebody is, and until the approval the sign-in screen says what is being waited for
  instead of blaming the password.
- **An administrator lets them in.** Approving a waiting account activates it and e-mails that
  person the link to sign in. The roster can be narrowed to just the accounts that are waiting, so
  nobody has to be found by reading the whole list. Approving hands out no rights beyond reading —
  raising a role stays a separate, deliberate edit — approving twice is harmless, and a blocked
  account is not approved but unblocked, which is its own action.
- **The instance's own settings have a screen.** An administrator opens *Nastavení* and decides
  there whether self-service registration is open, what the shared secret is — shown as readable
  text, because the whole job with it is to read it back and tell people what it is — and what
  greeting a newcomer will be met with on their first sign-in, written in Markdown with a live
  preview beside it that renders exactly what the welcome will. Registration cannot be opened while the secret is still empty, and nothing is written until
  it is saved.
- **The first sign-in is met with a welcome, once.** Whoever signs in for the first time gets the
  greeting the administrator wrote, and then the one question the app cannot answer on its own:
  which person of the photographs they are. It is a search over the library's people showing each
  candidate's face as well as their name, because a family archive is full of namesakes; picking one
  only marks it, and nothing is linked until it is confirmed. Saying so is what makes *moje fotky*,
  `person:me` in a search and one's own face beside one's comments start working — and somebody who
  would rather not is one click from skipping it. An account that already named a person is shown
  who, not asked again. However it ends, it does not come back.
- **Nobody has to be told their own password.** When somebody forgets theirs, an administrator
  starts a reset instead of inventing a password for them: Kukátko mails that person a one-time link,
  valid for a week, where they choose their own — and hands the administrator the same link to pass
  on by hand if the mailbox does not work. The page behind the link checks it before it asks for
  anything, so a link that has expired or been used says so plainly instead of refusing a filled-in
  form, and a new password is set without signing anybody in. Using it signs out every session of
  that account, the link works exactly once, and issuing a new one kills the old.
- **One dashboard that answers "what is in the library and is it healthy?"** — what the catalogue
  holds and what it weighs, what arrived in the last day/week/month/year, the backlogs still to work
  through (faces without a name, photos with no date or place, duplicates), the background queue
  broken down by job type and state with a per-type retry, and the health of everything it depends
  on: the database, the AI box, the last backup and import, the map provider's credits.
- **A library an AI agent can use:** Kukátko can expose itself as an **MCP server**, so an agent
  searches and organizes the library directly ("find grandma's photos from the sixties and put them
  in an album"). Off by default, authenticated with an ordinary API token, bound by the same RBAC —
  and nothing destructive is exposed.
- **Or the same job from the terminal.** `kukatko ctl photos` is the loop an agent needs per photo:
  read it whole (`get` — who is on it, and the text the recogniser read in it), actually look at it
  (`image`, streamed to a file), and write the evaluation back (`edit`) — with `--dry-run` to show
  its intent first, and `-o llm` to spend a fraction of the tokens the raw JSON would.
- **And the rest of the curation, too.** Albums and labels can be renamed, re-described and removed,
  not merely created; the several files one shot was stored as can be grouped into one tile and
  ungrouped again (a stack **groups, it never merges** — every file keeps its own metadata); a
  photo's crop, rotation, brightness and contrast can be read and rewritten without ever touching
  the original; smart albums can be saved, edited and deleted; a photo's visual neighbours can be
  listed with how alike they are, and a pair of them settled as the same shot or as two different
  ones. Deleting an album or a label refuses without an explicit `--yes` and offers a `--dry-run`
  first.
- **A photo's whole life fits in the terminal.** `kukatko ctl photos upload` streams files in
  through the ordinary ingest path — the same hashing, the same deduplication, the same follow-up
  work as the web uploader — and reports what was catalogued and what the library already had.
  `hide`, `archive` and their undos are ordinary commands, because each is undone by the one that
  reverses it. **What cannot be taken back is gated**: `photos purge`, `trash empty`,
  `trash purge-older` and the merge of a duplicate group all refuse to run without an explicit
  `--yes`, and every one of them offers a `--dry-run` that lists exactly which photos would be
  destroyed — a rehearsal that deliberately needs no confirmation, because seeing what is at stake
  is how the decision gets made. `ctl trash info` says what is in the trash and when retention will
  take each photo, so the decision is an informed one rather than a blind one. None of this is
  exposed to an MCP agent, and that difference is the point: the CLI is the door for a person
  holding a token, MCP is the door for an agent running unattended.
- **The conversation is readable from the terminal.** A photo's comment thread is often the only
  record of who is on it and when it was taken, so it can be read whole — bodies printed in full,
  not cut to a column — and written into. An agent writes under **its own account**, because the
  audit trail records the token's owner as the author and nobody should be able to put words in
  somebody else's mouth.
- **Naming people from the terminal too.** The whole recognition surface is there: list a photo's
  faces with the identities the machine suggests, attach one to a person (by name, which creates them
  if need be) or detach it, name a whole cluster of unnamed faces in one command, and record that a
  suggestion was wrong so it stops coming back. People can be created, renamed, merged and deleted —
  and because merging or deleting a person cannot be undone, both refuse without an explicit `--yes`,
  offer a `--dry-run` first, and report **who** they touched rather than a uid nobody can read.
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
- **Optional: an embeddings service** for semantic search and face recognition —
  [`kozaktomas/image-embeddings`](https://github.com/kozaktomas/image-embeddings) is the one this
  instance runs; see [below](#semantic-search-and-faces-optional). Everything except those two
  features works without it, and it is allowed to be offline.

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
`KUKATKO_WEB_SECURE_COOKIES: "true"`. The proxy also has to be inside `web.trusted_proxies`
(default: loopback + the private ranges, which covers a proxy on the same host or Docker network)
or Kukátko will attribute every request to the proxy itself — it only believes an
`X-Forwarded-For` that came *from* a listed network, so nobody else can pick their own address.

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

The two "smart" features need one thing Kukátko deliberately does not contain: a neural network.
Models are a different kind of dependency — a GPU, a couple of gigabytes of weights, a Python
runtime — and putting them inside the binary would make the binary a lie. So they live in a separate
HTTP service, and Kukátko is a client of it.

**[`kozaktomas/image-embeddings`](https://github.com/kozaktomas/image-embeddings)** is that service:
a small FastAPI app around **SigLIP 2 so400m/14** and **InsightFace buffalo_l**, with a Dockerfile
and a GPU deployment guide. It is what this instance runs.

```bash
git clone https://github.com/kozaktomas/image-embeddings && cd image-embeddings
docker build -t image-embeddings .
docker run -d -p 8000:8000 --name embeddings image-embeddings
```

Then point Kukátko at it — `KUKATKO_EMBEDDING_URL=http://embeddings:8000` when both run in the same
Docker network, or the address of whatever machine holds the GPU.

### How the two features actually work

**Semantic search** rests on SigLIP 2 being trained to put a picture and its description in the
*same* 1152-dimensional space. Every photo is embedded once, on upload (`POST /embed/image`), and the vector
is stored in a `halfvec` column with an HNSW cosine index. When you search, your words are embedded
by the same model (`POST /embed/text`) and the query becomes "give me the nearest vectors" — plain
SQL. That is why "sunset over water" finds the photo nobody ever titled, and why **similar photos**
is the same query with a photo's own vector as the needle. The default hybrid mode runs full-text
alongside it and merges, so exact words still win when you use them.

**Faces** are a second, unrelated model. `POST /embed/face` returns one 512-dimensional vector per
detected face plus its bounding box, and those vectors go into their own indexed column. Recognition
is then arithmetic on distances, and Kukátko deliberately stops short of acting on it:

- an unnamed face's nearest neighbours among **named** faces become *suggestions*, never assignments;
- faces nobody has named are grouped into **clusters** by union-find over their nearest neighbours,
  so you can name twenty photos of the same stranger in one go;
- a named person's faces have a centroid, and the ones sitting far from it are surfaced as
  **outliers** — that is how a misfiled face gets found;
- "not this person" is remembered as a **rejection**, so a wrong guess stays refused instead of
  coming back every sweep.

Nothing above writes an identity on its own. The machine narrows twenty thousand photos to one
question, and a human answers it — which is the whole design, not a missing feature.

**Text in photos** is a third use of the same service: `POST /ocr/image` reads what a photo's signs,
shop fronts and scanned pages say, and the text is stored on the photo and folded into the full-text
index at the lowest weight — findable, but never outranking something a person actually wrote. It runs
on stills only (no video poster frames), an image with no writing in it is recorded as exactly that so
it is never re-examined, and `embedding.ocr.enabled: false` turns the whole thing off.

**Without the service**, upload, browsing, full-text search, albums, labels, places, video and
everything else work as usual. `image_embed`, `face_detect` and `ocr` jobs simply queue in Postgres and
drain whenever the service comes back, so a GPU box that is powered off most of the time is a
supported setup rather than an outage. Search says so instead of pretending: semantic and hybrid
fall back to full-text and mark the result `degraded`.

Anything answering the same four endpoints will do — the contract is four JSON shapes:

- `POST /embed/image` — multipart, field `file` → `{ "dim": 1152, "embedding": [...] }`
- `POST /embed/text` — JSON `{ "text": "..." }` → the same shape, in the same vector space
- `POST /embed/face` — multipart, field `file` →
  `{ "faces_count": N, "faces": [{ "dim": 512, "embedding": [...], "bbox": [x1,y1,x2,y2], "det_score": 0.9 }] }`
- `POST /ocr/image` — multipart, field `file` (+ optional `min_confidence`) →
  `{ "text": "...", "blocks_count": N, "blocks": [{ "text": "...", "bbox": [x1,y1,x2,y2], "confidence": 0.99 }],
  "lang": "latin", "model": "..." }` — a photo with no text is an ordinary `200` with an empty result

Details, including how a bounding box is normalized against EXIF orientation, are in
[`docs/ARCHITECTURE.md` §6](docs/ARCHITECTURE.md).

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
