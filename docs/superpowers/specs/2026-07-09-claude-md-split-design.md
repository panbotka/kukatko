# CLAUDE.md → rules + index, detail into `docs/`

**Date:** 2026-07-09
**Status:** approved, ready for planning

## Problem

`CLAUDE.md` is 1866 lines / 173 KB (~45k tokens). It is loaded into every session, so
that cost is paid on every task before any work begins.

One section, `## Struktura a příkazy (scaffold M0)`, accounts for 1749 of the 1866 lines
(94%). It is an exhaustive package-by-package and endpoint-by-endpoint inventory of the
codebase. The remaining ~120 lines are the actual project rules.

The inventory is the worst possible content for this file:

- It is the most expensive part, and it is paid unconditionally.
- It drifts from the code, because nothing verifies it.
- It is re-derivable — an agent that needs it can read the source.
- It is partly duplicated in `README.md` (1789 lines) and `docs/ARCHITECTURE.md` (672 lines).

Worse, the Definition of Done actively causes the growth: step 1 instructs every task to
*"Aktualizuj `README.md` a `CLAUDE.md`"*. That is how the file reached 1866 lines.

## Goal

`CLAUDE.md` becomes **rules plus a routing table**: ~230 lines, ~6k tokens. The extracted
detail moves *verbatim* into four topic documents that an agent reads on demand.

Non-goal: rewriting, condensing, or fact-checking the moved prose. This is a pure move.

## Design

### Note: `@file` imports are not an option

`@docs/…md` lines in `CLAUDE.md` are inlined at load time. Splitting the file and
importing the pieces back would move text without reducing context cost. Only pointers
the agent chooses to follow save tokens. The routing table is therefore prose, not imports.

### New `CLAUDE.md` structure

| Section | Fate | ~Lines |
| --- | --- | --- |
| `## Co to je` | unchanged | 5 |
| `## Tech stack (závazné)` | unchanged | 10 |
| `## Kde co najdeš` | **new** — routing table | 12 |
| `## Mapa balíčků` | **new** — one line per package | 45 |
| `## Tvrdá brána kvality` | + one rule (see Guard) | 12 |
| `## Konfigurace` | rules only; key catalogue moves out | 12 |
| `## Databáze` | unchanged | 10 |
| `## Klíčové vzory` | unchanged | 12 |
| `## Definition of Done` | rewritten (see Guard) | 14 |
| `## Mimo rozsah`, `## Jazyk` | unchanged | 4 |

`## Struktura a příkazy (scaffold M0)` is deleted as a heading; its contents move out.

### Extraction map

Line numbers refer to `CLAUDE.md` at commit `deab0a5`.

| Source block | Lines | Destination |
| --- | --- | --- |
| `- **Layout:**` (Go packages) | 24–991 | `docs/PACKAGES.md` |
| `- **Frontend layout:**` | 992–1465 | `docs/FRONTEND.md` |
| `- **CLI:**` | 1466–1503 | `docs/OPERATIONS.md` |
| All `- **… API:**` bullets | 1504–1752 | `docs/API.md` |
| `- **Make cíle:**`, `- **CI/CD a balíčkování:**` | 1753–1772 | `docs/OPERATIONS.md` |
| `## Konfigurace` per-key catalogue | within 1785–1826 | `docs/OPERATIONS.md` |

Each new document gets a title and a one-paragraph preamble stating what it covers and
that it is descriptive reference, not rules. Body text is copied byte-for-byte.

`docs/OPERATIONS.md` is assembled in the order: CLI → configuration keys → Make targets
→ CI/CD and packaging.

### `## Kde co najdeš`

A table mapping task type to the single document to read. It must be precise enough that
an agent touching `internal/photos` opens `docs/PACKAGES.md` and nothing else. It covers
the four new documents and the four existing ones (`ARCHITECTURE.md`, `DEVELOPMENT.md`,
`PERF.md`, `RESTORE.md`), which today are referenced only in passing.

### `## Mapa balíčků`

One hand-written line per package: name, purpose, defining detail. Example:

```
- `internal/photos` — jádro foto-katalogu, Store nad pgx, dedup na SHA256 `file_hash`
- `internal/jobs`   — persistentní fronta jobů v Postgresu (retry, dedup, backoff)
```

Its purpose is to let the agent decide whether it needs `PACKAGES.md` at all. Hand-written
rather than generated: a list generated from package doc comments would either restate the
package name or drag back the detail being removed.

### Guard against re-growth

Three parts. A rule an agent can violate silently is not a rule.

1. **Explicit rule** in `## Tvrdá brána kvality`: *CLAUDE.md obsahuje jen pravidla
   a rozcestník. Popisné detaily patří do `docs/`.*

2. **Rewritten Definition of Done, step 1.** It currently says *"Aktualizuj `README.md`
   a `CLAUDE.md`"*. It becomes a routing instruction:
   - new/changed package → `docs/PACKAGES.md` (+ one line in `## Mapa balíčků`)
   - new/changed endpoint → `docs/API.md`
   - new config key → `docs/OPERATIONS.md` + `config.example.yaml`
   - new/changed component or hook → `docs/FRONTEND.md`
   - new CLI subcommand or Make target → `docs/OPERATIONS.md`
   - architectural change → `docs/ARCHITECTURE.md`
   - `CLAUDE.md` is touched **only** when a rule changes or a package is added/removed.

3. **Enforced budget.** A `docs-budget` Make target, wired into
   `check: fmt vet lint docs-budget test`, fails when `CLAUDE.md` exceeds **300 lines**:

   ```make
   ## docs-budget: Fail if CLAUDE.md grew beyond its rules+index budget.
   docs-budget:
   	@lines=$$(wc -l < CLAUDE.md); \
   	if [ "$$lines" -gt 300 ]; then \
   	  echo "CLAUDE.md má $$lines řádků (limit 300). Detaily patří do docs/."; exit 1; \
   	fi
   ```

   300 against a target of ~230 leaves headroom for the package map to grow with the
   project, while catching any attempt to paste a 60-line package description back in.
   No new dependency; pure shell.

## Explicitly out of scope

- **Rewriting or condensing the moved prose.** Zero information loss. A later task may trim.
- **`README.md`.** Its 1789 lines duplicate much of this content. Reconciling that overlap
  is a separate change with a far larger blast radius. Recorded as follow-up.
- **`@`-imports.** They inline at load time and save nothing.

## Verification

- `make check` passes, now including `docs-budget`.
- **Move fidelity:** extract the source line ranges from `git show HEAD:CLAUDE.md` and diff
  them against the bodies of the new documents. Expect an empty diff, modulo added titles
  and preambles.
- **Line accounting:** lines removed from `CLAUDE.md` ≈ lines added across the four new
  documents.
- **Budget met:** `wc -l CLAUDE.md` is under 300.
- **No dead links:** every path named in `## Kde co najdeš` exists on disk.
- **Guard works (negative test):** appending 100 lines to `CLAUDE.md` makes `make docs-budget`
  fail; revert.
- **Package map is complete:** every directory under `internal/` appears exactly once in
  `## Mapa balíčků`.
