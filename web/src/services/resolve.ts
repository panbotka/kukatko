import { instancePath } from './api'

/**
 * The client of `GET /api/v1/resolve/{kind}/{uid}` (`internal/resolveapi`):
 * which project an address from before projects existed means. The backend
 * answers with the project's slug only when the caller may enter it, and with
 * one and the same 404 for everything else.
 */

/** What an old link can name; the `{kind}` segment of the route. */
export type EntityKind = 'photo' | 'album' | 'label' | 'task' | 'subject' | 'notification'

/**
 * The slug of the project holding the `kind` entity `uid`, or `null` when there
 * is none the caller may enter (the backend's one 404).
 *
 * @throws Error for any other answer (rate limit, server error) — the caller
 *   then falls back on the entry rule rather than claiming the entity is gone.
 */
export async function resolveEntityProject(
  kind: EntityKind,
  uid: string,
  signal?: AbortSignal,
): Promise<string | null> {
  const res = await fetch(
    instancePath(`/resolve/${encodeURIComponent(kind)}/${encodeURIComponent(uid)}`),
    { credentials: 'same-origin', signal },
  )
  if (res.status === 404) {
    return null
  }
  if (!res.ok) {
    throw new Error(`resolve request failed: ${res.status}`)
  }
  const body = (await res.json()) as { slug: string }
  return body.slug
}
