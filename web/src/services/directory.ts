import { ApiError } from './auth'

/**
 * The people of the library: the accounts a signed-in person may name.
 *
 * It backs the one place a name has to be picked rather than typed — putting
 * somebody on a task, which is how a question reaches the one relative who would
 * know the answer.
 *
 * Deliberately separate from `services/users`, which is the *administration* of
 * accounts (roles, addresses, approval, passwords) and is admin-only. This is
 * the directory every role may read, and it carries only what a comment already
 * shows everybody: a name, and the uid its picture is fetched by.
 */

const API_BASE = '/api/v1'

/** One account as everybody may see it (`auth.DirectoryEntry`). */
export interface DirectoryUser {
  uid: string
  /** The display name, falling back to the username. */
  name: string
}

/** Response body of `GET /api/v1/users`. */
interface DirectoryResponse {
  users: DirectoryUser[]
}

/**
 * Lists the people of the library, ordered by the name they are shown under.
 *
 * @throws ApiError on a non-OK response (401 when the session has expired).
 */
export async function listDirectory(signal?: AbortSignal): Promise<DirectoryUser[]> {
  const res = await fetch(`${API_BASE}/users`, { credentials: 'same-origin', signal })
  if (!res.ok) {
    throw new ApiError(res.status, res.statusText || `request failed: ${res.status}`)
  }
  const body = (await res.json()) as DirectoryResponse
  return body.users
}
