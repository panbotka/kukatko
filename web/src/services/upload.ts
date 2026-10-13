import { ApiError } from './auth'
import { apiPath } from './api'

/**
 * Non-fatal condition reported alongside a created photo, mirroring the backend
 * `ingest.Warning` JSON shape (`internal/ingest/ingest.go`). `code` is a stable
 * machine identifier (for example `near_duplicate`) the UI can translate; the
 * `message` is the backend's human-readable fallback.
 */
export interface UploadWarning {
  code: string
  message: string
  photo_uid?: string
}

/** What happened to one uploaded file (`ingest.Outcome`). */
export type UploadOutcome = 'created' | 'duplicate' | 'error'

/**
 * Per-file result returned by `POST /api/v1/p/<slug>/upload`, mirroring the backend
 * `ingest.FileResult`. `status` carries HTTP-style per-file semantics (201
 * created, 409 duplicate, 413/415/500 error) so a single multi-file upload can
 * report mixed outcomes. `code` is the stable identifier of a refusal the UI can
 * translate (`not_media`, `unsupported_type`, `damaged`, `file_too_large`,
 * `quota_exceeded`); `error` is the server's raw text. A limit refusal carries
 * the limit that refused it in `limit_bytes` and, for the quota on the signed-in
 * upload, the project's stored bytes in `used_bytes` (the public upload link
 * sends neither for the quota).
 */
export interface UploadFileResult {
  filename: string
  status: number
  outcome: UploadOutcome
  photo_uid?: string
  code?: string
  error?: string
  limit_bytes?: number
  used_bytes?: number
  warnings?: UploadWarning[]
}

/** The refusal codes of the project limits, per file and for a whole request. */
export const LIMIT_CODES: ReadonlySet<string> = new Set(['file_too_large', 'quota_exceeded'])

/**
 * The multipart field declaring the next file's size (`ingest.SizeField`), so
 * the server can refuse a file over a limit before reading its bytes.
 */
export const SIZE_FIELD = 'size'

/** Response body of `POST /api/v1/p/<slug>/upload`: one result per uploaded file. */
export interface UploadResponse {
  results: UploadFileResult[]
}

/** Options for {@link uploadFile}. */
export interface UploadFileOptions {
  /** Called with upload progress as a fraction in `[0, 1]` as bytes are sent. */
  onProgress?: (fraction: number) => void
  /** Aborts the in-flight request when triggered. */
  signal?: AbortSignal
  /**
   * Where to post the file; defaults to the curator upload (`/api/v1/p/<slug>/upload`).
   * An upload link posts to `/api/v1/u/<code>/upload` instead.
   */
  url?: string
  /**
   * Plain form fields sent **before** the file — the backend reads the stream
   * once, in order, so a field after the file would arrive too late to apply.
   */
  fields?: Record<string, string>
}

/**
 * The shape of an upload function: {@link uploadFile} or anything bound to a
 * different endpoint, so {@link useUploadQueue} can drive either.
 */
export type UploadFn = (file: File, options?: UploadFileOptions) => Promise<UploadFileResult>

/**
 * Standard backend error envelope (`internal/ingest/http.go`). A request-level
 * 413 names the limit in `error` and may carry its numbers.
 */
interface ErrorBody {
  error?: string
  limit_bytes?: number
  used_bytes?: number
}

/**
 * Turns a request-level 413 limit refusal (`{"error":"quota_exceeded",…}`,
 * answered before the server read the body) into the same per-file error
 * result a refusal inside the batch would be, so the queue marks the file with
 * its reason instead of a bare failure. `undefined` for anything else.
 */
function limitRefusal(
  status: number,
  body: unknown,
  filename: string,
): UploadFileResult | undefined {
  if (status !== 413 || body === null || typeof body !== 'object') {
    return undefined
  }
  const { error, limit_bytes: limitBytes, used_bytes: usedBytes } = body as ErrorBody
  if (typeof error !== 'string' || !LIMIT_CODES.has(error)) {
    return undefined
  }
  return {
    filename,
    status,
    outcome: 'error',
    code: error,
    error,
    limit_bytes: typeof limitBytes === 'number' ? limitBytes : undefined,
    used_bytes: typeof usedBytes === 'number' ? usedBytes : undefined,
  }
}

/** Builds the DOMException used to signal an aborted upload (matches fetch). */
function abortError(): DOMException {
  return new DOMException('upload aborted', 'AbortError')
}

/** True when an error is the abort signal raised by {@link uploadFile}. */
export function isAbortError(error: unknown): boolean {
  return error instanceof DOMException && error.name === 'AbortError'
}

/**
 * Parses an XHR response body into an object regardless of `responseType`. With
 * `responseType = 'json'` the parsed object is on `xhr.response`; otherwise the
 * raw text is parsed defensively. Returns `null` when the body is empty or not
 * JSON so callers can fall back to the status line.
 */
function parseBody(xhr: XMLHttpRequest): unknown {
  const response: unknown = xhr.response
  if (response !== null && typeof response === 'object') {
    return response
  }
  if (typeof response === 'string' && response !== '') {
    try {
      return JSON.parse(response)
    } catch {
      return null
    }
  }
  return null
}

/**
 * Extracts the first per-file result from a successful upload body, or
 * `undefined` when the body does not carry a non-empty `results` array.
 *
 * Deliberately total: a 2xx response that does not match the documented shape
 * (a proxy's HTML/JSON error page, a truncated body, a future API change) must
 * settle the upload as an error. Indexing blindly would throw inside the XHR
 * callback, where nothing catches it — the promise would stay pending forever,
 * hanging the queue item and burning a concurrency slot.
 */
function firstResult(body: unknown): UploadFileResult | undefined {
  if (body === null || typeof body !== 'object') {
    return undefined
  }
  const { results } = body as Partial<UploadResponse>
  if (!Array.isArray(results) || results.length === 0) {
    return undefined
  }
  const first: unknown = results[0]
  return first !== null && typeof first === 'object' ? (first as UploadFileResult) : undefined
}

/** Extracts the `{ error }` message from a parsed error envelope, if present. */
function errorMessage(body: unknown): string | undefined {
  if (body !== null && typeof body === 'object' && 'error' in body) {
    const value = (body as ErrorBody).error
    if (typeof value === 'string' && value !== '') {
      return value
    }
  }
  return undefined
}

/**
 * Uploads a single file to `POST /api/v1/p/<slug>/upload` and resolves with its per-file
 * result. One file per request keeps progress accurate and lets the caller cap
 * concurrency by scheduling many independent uploads.
 *
 * Uses `XMLHttpRequest` rather than `fetch` because only XHR exposes upload
 * progress events. The session cookie is sent automatically (same-origin). The
 * file is streamed by the browser as multipart form data — never buffered whole
 * in JS — and the field name is irrelevant to the backend, which ingests every
 * part that carries a filename.
 *
 * @throws ApiError when the request itself fails (network error, 4xx/5xx, or a
 *   malformed response). Per-file `error` outcomes resolve normally with an
 *   `UploadFileResult` whose `outcome` is `'error'`.
 * @throws DOMException `AbortError` when `signal` is aborted (see
 *   {@link isAbortError}).
 */
export function uploadFile(file: File, options: UploadFileOptions = {}): Promise<UploadFileResult> {
  const { onProgress, signal, url = apiPath('/upload'), fields = {} } = options

  return new Promise<UploadFileResult>((resolve, reject) => {
    if (signal?.aborted) {
      reject(abortError())
      return
    }

    const form = new FormData()
    for (const [name, value] of Object.entries(fields)) {
      form.append(name, value)
    }
    // Right before the file it describes: the server reads the stream once, in
    // order, and refuses an oversized file from this alone.
    form.append(SIZE_FIELD, String(file.size))
    form.append('files', file, file.name)

    const xhr = new XMLHttpRequest()
    xhr.open('POST', url)
    xhr.responseType = 'json'
    // Same-origin cookies are sent regardless; explicit for clarity.
    xhr.withCredentials = true

    const onAbort = (): void => {
      xhr.abort()
    }
    if (signal) {
      signal.addEventListener('abort', onAbort)
    }
    const cleanup = (): void => {
      if (signal) {
        signal.removeEventListener('abort', onAbort)
      }
    }

    if (onProgress) {
      xhr.upload.onprogress = (event: ProgressEvent): void => {
        if (event.lengthComputable && event.total > 0) {
          onProgress(event.loaded / event.total)
        }
      }
    }

    xhr.onload = (): void => {
      cleanup()
      // Everything here runs inside an XHR callback, outside the promise
      // executor: an escaping throw would never reject and the upload would
      // hang forever. Settle it as an error instead.
      try {
        const body = parseBody(xhr)
        const refusal = limitRefusal(xhr.status, body, file.name)
        if (refusal !== undefined) {
          resolve(refusal)
          return
        }
        if (xhr.status < 200 || xhr.status >= 300) {
          const message =
            errorMessage(body) ?? (xhr.statusText || `upload failed: ${String(xhr.status)}`)
          reject(new ApiError(xhr.status, message))
          return
        }
        const result = firstResult(body)
        if (result === undefined) {
          reject(new ApiError(xhr.status, 'upload returned no result'))
          return
        }
        resolve(result)
      } catch (err: unknown) {
        reject(new ApiError(xhr.status, err instanceof Error ? err.message : 'malformed response'))
      }
    }

    xhr.onerror = (): void => {
      cleanup()
      reject(new ApiError(0, 'network error'))
    }

    xhr.onabort = (): void => {
      cleanup()
      reject(abortError())
    }

    xhr.send(form)
  })
}
