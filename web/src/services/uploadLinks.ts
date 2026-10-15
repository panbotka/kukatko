import { ApiError } from './auth'
import { uploadFile, type UploadFn } from './upload'
import { apiPath, instancePath } from './api'

/**
 * Upload links (`internal/uploadlinkapi`): a short link a curator posts to a
 * group chat, through which anybody — signed in or not — uploads photos into
 * preset albums and labels.
 *
 * The management half (`/upload-links`) is for curators and up; the public half
 * (`/u/<code>`) needs no session at all, and its upload goes through the same
 * XHR {@link uploadFile} the curator upload uses, pointed at the link.
 */

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
  /** Empty once the account is gone, and for a former member (`created_by_former`). */
  created_by_name: string
  /**
   * The creator is no member of the project any more: shown as a former member,
   * never by their live name.
   */
  created_by_former?: boolean
  created_at: string
  expires_at: string
  revoked_at: string | null
  upload_count: number
  last_used_at: string | null
  albums: UploadLinkTarget[]
  labels: UploadLinkTarget[]
  state: UploadLinkState
  /**
   * The short code, present only when the caller manages the link (its creator
   * or an administrator) and the code is known. A link created before codes
   * were stored readably has none until its original code is restored.
   */
  code?: string
  /** The frontend path of the public page, `/u/<code>`; present exactly when `code` is. */
  path?: string
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
 * A freshly created link with its code. The link stays copyable from the list
 * afterwards; `code` and `path` merely repeat what `link` carries.
 */
export interface CreatedUploadLink {
  link: UploadLink
  code: string
  /** The frontend path of the public page, `/u/<code>`. */
  path: string
}

/**
 * What anybody holding a link learns about it: names, the expiry and whether
 * registering through it could succeed, nothing else.
 */
export interface PublicUploadLink {
  title: string
  /** The name of the project the photos go into; the page says it under the title. */
  project: string
  note: string
  albums: string[]
  labels: string[]
  expires_at: string
  /**
   * Whether the link's project takes self-service registrations — the switch a
   * registration through this link is judged by, so the page offers one only
   * where it can succeed.
   */
  registration_enabled: boolean
  /**
   * Whether the link's project has used up its storage quota: nothing can be
   * uploaded until it has room again. A yes/no only — the page learns no sizes.
   */
  storage_full: boolean
  /**
   * Whether an upload from this browser is credited to the signed-in account —
   * only an approved member of the link's project is. Anybody else (anonymous,
   * or signed in but no member of the project) uploads under the name they type.
   */
  credits_account?: boolean
}

/** Standard backend error envelope. */
interface ErrorBody {
  error?: string
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
  const res = await fetch(apiPath(path), {
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
 * Makes the original code of a link created before codes were stored readably
 * known again. The backend stores it only when it matches the link's hash, so
 * the URL does not change; any other code is refused with a 422 {@link ApiError}.
 */
export async function restoreUploadLinkCode(
  uid: string,
  code: string,
  signal?: AbortSignal,
): Promise<UploadLink> {
  const body = await sendJSON<{ link: UploadLink }>(
    'POST',
    `/upload-links/${encodeURIComponent(uid)}/restore-code`,
    { code },
    signal,
  )
  return body.link
}

/** Gives a link a fresh code; its old URL stops working at once. */
export async function newUploadLinkCode(uid: string, signal?: AbortSignal): Promise<UploadLink> {
  const body = await sendJSON<{ link: UploadLink }>(
    'POST',
    `/upload-links/${encodeURIComponent(uid)}/new-code`,
    undefined,
    signal,
  )
  return body.link
}

/**
 * Reads a code out of what somebody pasted: the bare code, or a whole link whose
 * path holds `/u/<code>` (query, fragment and a trailing slash dropped).
 */
export function codeFromInput(input: string): string {
  const value = input.trim()
  const at = value.indexOf('/u/')
  if (at < 0) {
    return value
  }
  return value
    .slice(at + 3)
    .split(/[?#]/)[0]
    .replace(/\/$/, '')
}

/**
 * Reads the public description of the link `code` names. It throws an
 * {@link ApiError} otherwise — 404 alike for an unknown code and an expired or
 * revoked link, which the server deliberately does not tell apart.
 */
export async function fetchPublicUploadLink(
  code: string,
  signal?: AbortSignal,
): Promise<PublicUploadLink> {
  const res = await fetch(instancePath(`/u/${encodeURIComponent(code)}`), {
    credentials: 'same-origin',
    signal,
  })
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
      url: instancePath(`/u/${encodeURIComponent(code)}/upload`),
      fields: { name: uploaderName().trim() },
    })
}

/** The absolute URL of a link's public page, for copying and sharing. */
export function publicLinkURL(path: string): string {
  return new URL(path, window.location.origin).toString()
}
