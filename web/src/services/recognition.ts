import { ApiError } from './auth'
import { type Candidate, type CandidateCounts } from './faces'
import { type Subject } from './people'

/**
 * Client for the recognition sweep (`GET /api/v1/faces/sweep`), which scans every
 * named person for confident matches among unnamed faces and streams the result as
 * newline-delimited JSON. Streaming lets the page show results person by person as
 * they arrive instead of after the whole (slow) scan — the wait is the worst part of
 * the feature.
 *
 * It is read-only: confirming a candidate goes through {@link import('./people').assignFace},
 * rejecting one through {@link import('./feedback').rejectFace}. The sweep never
 * auto-assigns; confidence only narrows the list.
 */

const API_BASE = '/api/v1'

/** Standard backend error envelope shared by every API group. */
interface ErrorBody {
  error?: string
}

/** Extracts the backend error message from a non-OK response, if present. */
async function readErrorMessage(res: Response): Promise<string> {
  try {
    const body = (await res.json()) as ErrorBody
    if (typeof body.error === 'string' && body.error !== '') {
      return body.error
    }
  } catch {
    // Body was empty or not JSON; fall back to the status text below.
  }
  return res.statusText || `request failed: ${String(res.status)}`
}

/** The confidence (as a percentage) and per-person limit for a sweep. */
export interface SweepParams {
  /** How sure a match must be, as a percentage; the backend maps it to a distance. */
  confidence: number
  /** Cap on candidates per person; 0 means all. */
  limit: number
}

/** Running scan position, emitted once per scanned subject. */
export interface SweepProgress {
  scanned: number
  total: number
  name: string
}

/** A subject with at least one actionable candidate, in the per-subject search shape. */
export interface SweepPerson {
  subject: Subject
  candidates: Candidate[]
  counts: CandidateCounts
  actionable: number
}

/** The sweep's global tally, emitted last. */
export interface SweepSummary {
  people_scanned: number
  people_with_matches: number
  total_actionable: number
  total_already_done: number
  capped: boolean
  subjects_total: number
}

/** One streamed sweep message, discriminated by `type`. */
export type SweepMessage =
  | { type: 'progress'; progress: SweepProgress }
  | { type: 'person'; person: SweepPerson }
  | { type: 'summary'; summary: SweepSummary }

/** parseLine turns one NDJSON line into a typed sweep message. */
function parseLine(line: string): SweepMessage {
  return JSON.parse(line) as SweepMessage
}

/**
 * streamSweep opens the sweep stream and invokes onMessage for each parsed line as it
 * arrives. It resolves when the stream ends (after the summary line) and rejects on a
 * non-OK response or a network error; aborting `signal` cancels the request, surfacing
 * as an AbortError the caller can ignore. onMessage is only ever called with a fully
 * parsed line, so partial chunks never leak to the caller.
 */
export async function streamSweep(
  params: SweepParams,
  onMessage: (message: SweepMessage) => void,
  signal?: AbortSignal,
): Promise<void> {
  const query = `confidence=${String(params.confidence)}&limit=${String(params.limit)}`
  const res = await fetch(`${API_BASE}/faces/sweep?${query}`, {
    credentials: 'same-origin',
    signal,
  })
  if (!res.ok) {
    throw new ApiError(res.status, await readErrorMessage(res))
  }
  if (res.body === null) {
    return
  }
  const reader = res.body.getReader()
  const decoder = new TextDecoder()
  let buffer = ''
  try {
    for (;;) {
      const { done, value } = await reader.read()
      if (done) {
        break
      }
      buffer += decoder.decode(value, { stream: true })
      buffer = drainLines(buffer, onMessage)
    }
    const tail = buffer.trim()
    if (tail !== '') {
      onMessage(parseLine(tail))
    }
  } finally {
    reader.releaseLock()
  }
}

/**
 * drainLines emits every complete newline-terminated line in buffer via onMessage and
 * returns the unconsumed remainder (a partial line still awaiting more bytes).
 */
function drainLines(buffer: string, onMessage: (message: SweepMessage) => void): string {
  let rest = buffer
  let newline = rest.indexOf('\n')
  while (newline !== -1) {
    const line = rest.slice(0, newline).trim()
    rest = rest.slice(newline + 1)
    if (line !== '') {
      onMessage(parseLine(line))
    }
    newline = rest.indexOf('\n')
  }
  return rest
}
