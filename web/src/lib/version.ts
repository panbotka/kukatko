import type { VersionInfo } from '../services/capabilities'

/** Where a commit hash resolves to a page a human can read. The repo is public. */
const COMMIT_URL_BASE = 'https://github.com/panbotka/kukatko/commit/'

/**
 * What a git commit hash may look like before it is pasted into a URL: 7–40 hex
 * characters. The value comes from our own linker flags, so this is not a
 * security boundary — it just keeps a placeholder from becoming a link that
 * leads nowhere, which is exactly what a development build reports (`none`).
 */
const COMMIT_PATTERN = /^[0-9a-f]{7,40}$/i

/**
 * Formats a build's version for display: a semantic version gets the customary
 * `v` prefix (`0.5.1` → `v0.5.1`), anything else is shown as-is — notably the
 * `dev` placeholder an un-stamped build reports, which a developer running a
 * local binary should see for what it is rather than as a made-up number.
 *
 * Returns `null` when there is nothing to show (no build metadata yet, or an
 * empty version), so a caller can render nothing rather than an empty line.
 */
export function formatVersion(info: VersionInfo | undefined): string | null {
  const raw = info?.version.trim() ?? ''
  if (raw === '') {
    return null
  }
  return /^\d/.test(raw) ? `v${raw}` : raw
}

/**
 * Returns the URL of the commit a build was made from, or `null` when the commit
 * is not a real hash (a development build's `none`, an empty value, or anything
 * that is not a hex sha) — the caller then shows no link at all.
 */
export function commitUrl(info: VersionInfo | undefined): string | null {
  const raw = info?.commit.trim() ?? ''
  if (!COMMIT_PATTERN.test(raw)) {
    return null
  }
  return `${COMMIT_URL_BASE}${raw}`
}
