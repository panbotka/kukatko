/**
 * How a number on the system dashboard should read at a glance.
 *
 * The page is a wall of numeric columns, and every one of them used to be
 * painted the same: a queue with three dead jobs among a hundred thousand
 * finished ones looked exactly like an idle one. The tones below are the whole
 * vocabulary — one rule, applied by every panel of the page, so the same state
 * never gets two different colours in two tables.
 */
export type CountTone =
  /** Work waiting to be picked up. */
  | 'queued'
  /** Work happening right now. */
  | 'running'
  /** Work that did not succeed — a retryable failure or a dead letter alike. */
  | 'failed'
  /** Work that finished. History: true, and deliberately not eye-catching. */
  | 'done'
  /** Not a queue state at all — a plain fact about the library. */
  | 'plain'

/**
 * The backend's job lifecycle states, mapped onto the tone each is painted in.
 * `failed` and `dead` share one: to a reader they are the same bad news, only
 * one of them has run out of retries, and the row's own requeue button is what
 * tells them apart.
 */
const STATE_TONES: Record<string, CountTone | undefined> = {
  queued: 'queued',
  running: 'running',
  failed: 'failed',
  dead: 'failed',
  done: 'done',
}

/**
 * The tone a queue state is painted in. An unknown state — a lifecycle value
 * shipped before this table learned about it — stays plain rather than guessing
 * a colour that would mean something specific.
 */
export function toneForJobState(state: string): CountTone {
  return STATE_TONES[state] ?? 'plain'
}

/**
 * What a zero wears. Muted, never coloured: the point of the whole scale is that
 * the eye falls on the non-zero values, and a page of coloured zeroes says
 * nothing louder than a page of black ones.
 */
const ZERO_CLASS = 'text-secondary'

/**
 * The classes each tone paints a non-zero number in. The colours themselves live
 * in `tokens.css` (`.kk-count--*`) rather than in a Bootstrap `text-*` utility,
 * because the page paints its numbers on **two very different plates** — a table
 * cell on the near-black page tone, a tile on Superhero's mid-slate `.card` fill
 * — and one fixed colour cannot clear the contrast bar on both. The tone is one;
 * how light it has to be painted is the stylesheet's business.
 *
 * `running` also pulses, because colour alone cannot say *in progress* — the
 * other states are coloured too — and a count that is quietly moving should look
 * like it.
 */
const TONE_CLASSES: Record<CountTone, string> = {
  queued: 'kk-count--queued fw-semibold',
  running: 'kk-count--running fw-semibold',
  failed: 'kk-count--failed fw-semibold',
  done: ZERO_CLASS,
  plain: '',
}

/**
 * The classes a number takes given what it counts and how many there are — the
 * one rule the whole `/system` page colours by.
 *
 * Zero is always muted and never coloured, whatever it counts. Finished work is
 * muted too: it is history, it only grows, and it must not compete with the
 * three states somebody can actually act on.
 */
export function countToneClass(tone: CountTone, value: number): string {
  return value <= 0 ? ZERO_CLASS : TONE_CLASSES[tone]
}

/**
 * Whether the number should carry a warning glyph beside it. Colour is never the
 * only carrier of the message: the state is named in the column header or the
 * row label, and a failure that is actually there gets a second, non-colour
 * signal on top.
 */
export function countToneAlerts(tone: CountTone, value: number): boolean {
  return tone === 'failed' && value > 0
}
