import { ApiError } from './auth'

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
 * Per-file result returned by `POST /api/v1/upload`, mirroring the backend
 * `ingest.FileResult`. `status` carries HTTP-style per-file semantics (201
 * created, 409 duplicate, 413/500 error) so a single multi-file upload can
 * report mixed outcomes.
 */
export interface UploadFileResult {
  filename: string
  status: number
  outcome: UploadOutcome
  photo_uid?: string
  error?: string
  warnings?: UploadWarning[]
}

/** Response body of `POST /api/v1/upload`: one result per uploaded file. */
export interface UploadResponse {
  results: UploadFileResult[]
}

/** Options for {@link uploadFile}. */
export interface UploadFileOptions {
  /** Called with upload progress as a fraction in `[0, 1]` as bytes are sent. */
  onProgress?: (fraction: number) => void
  /** Aborts the in-flight request when triggered. */
  signal?: AbortSignal
}

const API_BASE = '/api/v1'

/** Standard backend error envelope (`internal/ingest/http.go`). */
interface ErrorBody {
  error?: string
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
 * Uploads a single file to `POST /api/v1/upload` and resolves with its per-file
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
  const { onProgress, signal } = options

  return new Promise<UploadFileResult>((resolve, reject) => {
    if (signal?.aborted) {
      reject(abortError())
      return
    }

    const form = new FormData()
    form.append('files', file, file.name)

    const xhr = new XMLHttpRequest()
    xhr.open('POST', `${API_BASE}/upload`)
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
