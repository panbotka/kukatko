import { ApiError } from './auth'
import { uploadFile, type UploadFn } from './upload'

/**
 * Upload links (`internal/uploadlinkapi`): a short link a curator posts to a
 * group chat, through which anybody — signed in or not — uploads photos into
 * preset albums and labels.
 *
 * The management half (`/upload-links`) is for curators and up; the public half
 * (`/u/<code>`) needs no session at all, and its upload goes through the same
 * XHR {@link uploadFile} the curator upload uses, pointed at the link.
 */

const API_BASE = '/api/v1'

/** Where a link stands in its life (`uploadlink.State`). */
export type UploadLinkState = 'active' | 'expired' | 'revoked'

/** One album or label a link files its photos into. */
export interface UploadLinkTarget {
  uid: string
  name: string
}

/** One link as the management routes return it. */
export interface UploadLink {
  uid: string
  title: string
  note: string
  created_by: string | null
  created_by_name: string
  created_at: string
  expires_at: string
  revoked_at: string | null
  upload_count: number
  last_used_at: string | null
  albums: UploadLinkTarget[]
  labels: UploadLinkTarget[]
  state: UploadLinkState
}

/** `GET /upload-links`: the links plus the validity bounds the forms offer. */
export interface UploadLinkList {
  links: UploadLink[]
  default_days: number
  max_days: number
}

/** What a curator asks for when creating a link. */
export interface UploadLinkInput {
  title: string
  note: string
  album_uids: string[]
  label_uids: string[]
  valid_days: number
}

/**
 * A freshly created link: the only response that ever carries the code — the
 * backend stores just its hash, so a lost link cannot be shown again, only
 * replaced by a new one.
 */
export interface CreatedUploadLink {
  link: UploadLink
  code: string
  /** The frontend path of the public page, `/u/<code>`. */
  path: string
}

/** What anybody holding a link learns about it: names and the expiry, nothing else. */
export interface PublicUploadLink {
  title: string
  note: string
  albums: string[]
  labels: string[]
  expires_at: string
}

/**
 * The public page's answer for a link that exists but no longer accepts
 * uploads (HTTP 410), saying which of the two happened.
 */
export class UploadLinkGoneError extends Error {
  readonly state: Exclude<UploadLinkState, 'active'>

  constructor(state: Exclude<UploadLinkState, 'active'>) {
    super(`upload link is ${state}`)
    this.name = 'UploadLinkGoneError'
    this.state = state
  }
}

/** Standard backend error envelope, plus the 410's state. */
interface ErrorBody {
  error?: string
  state?: string
}

/** Reads the backend error message from a failed response, falling back to the status. */
async function readError(res: Response): Promise<ErrorBody> {
  try {
    return (await res.json()) as ErrorBody
  } catch {
    return {}
  }
}

/** Throws the ApiError a failed response stands for. */
async function fail(res: Response): Promise<never> {
  const body = await readError(res)
  const message =
    typeof body.error === 'string' && body.error !== ''
      ? body.error
      : res.statusText || `request failed: ${String(res.status)}`
  throw new ApiError(res.status, message)
}

/** Sends a JSON request to the management routes and decodes the JSON answer. */
async function sendJSON<T>(
  method: string,
  path: string,
  body?: unknown,
  signal?: AbortSignal,
): Promise<T> {
  const res = await fetch(`${API_BASE}${path}`, {
    method,
    credentials: 'same-origin',
    headers: body === undefined ? undefined : { 'Content-Type': 'application/json' },
    body: body === undefined ? undefined : JSON.stringify(body),
    signal,
  })
  if (!res.ok) {
    return fail(res)
  }
  return (await res.json()) as T
}

/** Lists the caller's links (an administrator's: everybody's), newest first. */
export function fetchUploadLinks(signal?: AbortSignal): Promise<UploadLinkList> {
  return sendJSON<UploadLinkList>('GET', '/upload-links', undefined, signal)
}

/** Creates a link; the answer carries its code, once. */
export function createUploadLink(
  input: UploadLinkInput,
  signal?: AbortSignal,
): Promise<CreatedUploadLink> {
  return sendJSON<CreatedUploadLink>('POST', '/upload-links', input, signal)
}

/** Makes a link valid for `validDays` days from now. */
export async function extendUploadLink(
  uid: string,
  validDays: number,
  signal?: AbortSignal,
): Promise<UploadLink> {
  const body = await sendJSON<{ link: UploadLink }>(
    'POST',
    `/upload-links/${encodeURIComponent(uid)}/extend`,
    { valid_days: validDays },
    signal,
  )
  return body.link
}

/** Revokes a link for good. */
export async function revokeUploadLink(uid: string, signal?: AbortSignal): Promise<UploadLink> {
  const body = await sendJSON<{ link: UploadLink }>(
    'POST',
    `/upload-links/${encodeURIComponent(uid)}/revoke`,
    undefined,
    signal,
  )
  return body.link
}

/**
 * Reads the public description of the link `code` names. It throws
 * {@link UploadLinkGoneError} for an expired or revoked link and an
 * {@link ApiError} (404 for an unknown code) otherwise.
 */
export async function fetchPublicUploadLink(
  code: string,
  signal?: AbortSignal,
): Promise<PublicUploadLink> {
  const res = await fetch(`${API_BASE}/u/${encodeURIComponent(code)}`, {
    credentials: 'same-origin',
    signal,
  })
  if (res.status === 410) {
    const body = await readError(res)
    throw new UploadLinkGoneError(body.state === 'revoked' ? 'revoked' : 'expired')
  }
  if (!res.ok) {
    return fail(res)
  }
  return (await res.json()) as PublicUploadLink
}

/**
 * Returns an upload function that sends each file through the link `code`,
 * stamped with the uploader's typed name (read when each file starts, so a name
 * typed mid-batch applies to the files still waiting).
 */
export function linkUploader(code: string, uploaderName: () => string): UploadFn {
  return (file, options = {}) =>
    uploadFile(file, {
      ...options,
      url: `${API_BASE}/u/${encodeURIComponent(code)}/upload`,
      fields: { name: uploaderName().trim() },
    })
}

/** The absolute URL of a link's public page, for copying and sharing. */
export function publicLinkURL(path: string): string {
  return new URL(path, window.location.origin).toString()
}
