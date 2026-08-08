import { type LabelCount } from '../services/organize'

import { foldedIncludes, foldText } from './text'

/**
 * Browsing the labels index: the pure search/sort/grouping the page lays over
 * the one list the API returns.
 *
 * The API hands over every label at once and the page used to draw each of them
 * as a full-width row — a hundred rows carrying one word and a number, five
 * screens tall, ordered by nothing but the alphabet. Worse, a real library grows
 * whole families of numbered labels (`Dum11`, `Dum12`, `Dum20`, …) that share a
 * prefix and, sorted by name, occupy the entire beginning of the list. So the
 * page searches, orders by photo count and folds those families into one
 * expandable entry — and the rules live here as pure functions, testable without
 * a DOM, exactly as `lib/albumBrowse` and `lib/peopleBrowse` do for their
 * indexes.
 */

/** The orderings the sort selector offers. */
export const LABEL_SORTS = ['count', 'name'] as const

/** One ordering of the labels index. */
export type LabelSort = (typeof LABEL_SORTS)[number]

/**
 * The ordering used when the URL asks for none: the most-used labels first.
 *
 * The album and people indexes open alphabetically, this one does not: a label
 * is a word somebody typed, and the alphabet is the one order that guarantees
 * the numbered house-number families come first. Photo count puts the labels
 * that actually carry the library at the top.
 */
export const LABEL_SORT_DEFAULT: LabelSort = 'count'

/** Narrows a raw URL value to an ordering, defaulting when it is not one. */
export function toLabelSort(value: string): LabelSort {
  return (LABEL_SORTS as readonly string[]).includes(value)
    ? (value as LabelSort)
    : LABEL_SORT_DEFAULT
}

/**
 * URL-encoded view state of the labels index: the name search, the ordering and
 * which numbered families are expanded. All values are strings (the urlState
 * convention), so the whole view round-trips through the query string.
 */
// A type alias (not an interface) so it satisfies the urlState `Record<string,
// string>` constraint — interfaces lack the implicit index signature TS requires.
// eslint-disable-next-line @typescript-eslint/consistent-type-definitions -- see above
export type LabelsView = {
  q: string
  sort: string
  /** Comma-separated family keys the reader has opened. */
  open: string
}

/** The labels index as it opens: everything, most-used first, families folded. */
export const LABELS_DEFAULTS: LabelsView = {
  q: '',
  sort: LABEL_SORT_DEFAULT,
  open: '',
}

/**
 * How many members a numbered family needs before it is folded into one entry.
 *
 * Below this it is not flooding anything — three chips take less room than the
 * chip that would hide them, and hiding a label the reader can already see is a
 * worse trade than showing one more.
 */
export const LABEL_FAMILY_MIN = 4

/** Separator between family keys in the `open` URL value. */
const OPEN_SEPARATOR = ','

/**
 * A name ending in digits, split into everything before them and the number.
 * The prefix must end in a non-digit, so a label that is *only* a number
 * (`2024`) has no family to belong to.
 */
const NUMBERED_NAME = /^(.*\D)\d+$/u

/** Trailing separators between a family's prefix and its number. */
const TRAILING_SEPARATORS = /[\s_·.-]+$/u

/**
 * The prefix a numbered label belongs under — `Dum11` and `Dum 12` both yield
 * `Dum` — or `null` for a name that is not a prefix plus a number.
 */
export function familyPrefix(name: string): string | null {
  const match = NUMBERED_NAME.exec(name.trim())
  if (match === null) {
    return null
  }
  const prefix = match[1].replace(TRAILING_SEPARATORS, '')
  return prefix === '' ? null : prefix
}

/**
 * The URL key of the family a label belongs to, or `null` for a label that is
 * not numbered. Folded ({@link foldText}), so `Dum11` and `dum 12` share one
 * family; commas are dropped because they separate keys in the URL value.
 */
export function familyKey(name: string): string | null {
  const prefix = familyPrefix(name)
  if (prefix === null) {
    return null
  }
  const key = foldText(prefix).replaceAll(OPEN_SEPARATOR, '')
  return key === '' ? null : key
}

/** The family keys the URL currently asks to be expanded. */
export function openFamilies(open: string): string[] {
  return open.split(OPEN_SEPARATOR).filter((key) => key !== '')
}

/** Encodes a set of expanded family keys back into the URL value. */
function encodeOpen(keys: readonly string[]): string {
  return keys.join(OPEN_SEPARATOR)
}

/** Expands a family if it is folded, folds it if it is open. */
export function toggleFamilyOpen(open: string, key: string): string {
  const keys = openFamilies(open)
  return keys.includes(key)
    ? encodeOpen(keys.filter((other) => other !== key))
    : encodeOpen([...keys, key])
}

/** Expands a family, leaving the already-open ones alone. */
export function withFamilyOpen(open: string, key: string): string {
  const keys = openFamilies(open)
  return keys.includes(key) ? open : encodeOpen([...keys, key])
}

/** What the labels index is currently showing, decoded from the URL. */
export interface LabelBrowseOptions {
  /** Free-text filter over names (folded: case- and accent-insensitive). */
  query: string
  /** The selected ordering. */
  sort: LabelSort
  /** The family keys the reader has expanded. */
  open: readonly string[]
  /** Active UI language, deciding how names collate. */
  language: string
}

/** Decodes the URL view into the options {@link browseLabels} takes. */
export function labelBrowseOptions(view: LabelsView, language: string): LabelBrowseOptions {
  return {
    query: view.q,
    sort: toLabelSort(view.sort),
    open: openFamilies(view.open),
    language,
  }
}

/** One label, standing on its own in the cloud. */
export interface LabelChipEntry {
  kind: 'label'
  /** Stable React key — the label's uid. */
  key: string
  label: LabelCount
}

/** A folded family of numbered labels, standing in for all of its members. */
export interface LabelFamilyEntry {
  kind: 'family'
  /** Stable React key and URL value — the folded prefix. */
  key: string
  /** The prefix as its members spell it, for display. */
  prefix: string
  /** Its members, in the selected order. */
  labels: LabelCount[]
  /** Photos across the whole family, so it can be ordered like a single label. */
  photoCount: number
  /** Whether the URL asks for it to be shown expanded. */
  expanded: boolean
}

/** One thing the cloud draws: a label, or a family standing in for several. */
export type LabelEntry = LabelChipEntry | LabelFamilyEntry

/** The labels index after searching: what to render, and what the search dropped. */
export interface LabelBrowseResult {
  /** The cloud's entries, in the selected order. */
  entries: LabelEntry[]
  /** How many labels the search kept. */
  matched: number
  /** How many labels the search dropped. */
  filteredOut: number
}

/**
 * Compares two names in the reader's own language: numeric collation so `Dum 2`
 * precedes `Dum 10`, base sensitivity so case and diacritics do not split the
 * alphabet.
 *
 * The API already orders by name, but in the database's collation — the page
 * re-sorts so a Czech reader gets a Czech alphabet, and so the tie-break under
 * "most photos" is the same order the alphabetical view uses.
 */
function compareNames(a: string, b: string, language: string): number {
  return a.localeCompare(b, language, { numeric: true, sensitivity: 'base' })
}

/** The name an entry sorts under: the label's, or the family's prefix. */
function sortName(entry: LabelEntry): string {
  return entry.kind === 'label' ? entry.label.name : entry.prefix
}

/** The photos an entry stands for: the label's, or the family's total. */
function photoCount(entry: LabelEntry): number {
  return entry.kind === 'label' ? entry.label.photo_count : entry.photoCount
}

/**
 * Applies the search, the family folding and the ordering to the whole label
 * list.
 *
 * A search **dissolves the families**: typing `dum4` is asking for `Dum4`,
 * `Dum41` and `Dum47` themselves, and answering it with a folded chip the reader
 * has to open again would be answering a different question. With no search the
 * families fold, so the numbered ones take one chip instead of dozens.
 *
 * `count` orders by `photo_count` — the figure each chip shows, and the one a
 * label's gallery honours — with the name as the tie-break, so the cloud never
 * reshuffles itself between renders. A family is ordered by its members' total,
 * so folding cannot bury a prefix that carries a thousand photos at the bottom.
 */
export function browseLabels(
  labels: LabelCount[],
  { query, sort, open, language }: LabelBrowseOptions,
): LabelBrowseResult {
  const pool = labels.filter((label) => foldedIncludes(label.name, query))
  const searching = foldText(query) !== ''
  const entries = searching ? pool.map(asChip) : fold(pool, open)

  const compare = (a: LabelEntry, b: LabelEntry) =>
    sort === 'count'
      ? photoCount(b) - photoCount(a) || compareNames(sortName(a), sortName(b), language)
      : compareNames(sortName(a), sortName(b), language)

  entries.sort(compare)
  for (const entry of entries) {
    if (entry.kind === 'family') {
      entry.labels.sort((a, b) => compare(asChip(a), asChip(b)))
    }
  }

  return { entries, matched: pool.length, filteredOut: labels.length - pool.length }
}

/** Wraps one label as a cloud entry of its own. */
function asChip(label: LabelCount): LabelChipEntry {
  return { kind: 'label', key: label.uid, label }
}

/**
 * Folds every numbered family of at least {@link LABEL_FAMILY_MIN} members into
 * a single entry, leaving every other label a chip of its own. A family that
 * does not reach the minimum is left dissolved — its members go back into the
 * cloud rather than hiding behind a chip that saves nothing.
 *
 * Mixed spellings (`Dum11`, `DUM12`) share one family; the prefix shown is the
 * one the first member of that family spells, which keeps the display stable for
 * a stable input order.
 */
function fold(labels: LabelCount[], open: readonly string[]): LabelEntry[] {
  const families = new Map<string, LabelFamilyEntry>()
  const entries: LabelEntry[] = []

  for (const label of labels) {
    const key = familyKey(label.name)
    if (key === null) {
      entries.push(asChip(label))
      continue
    }
    const family = families.get(key)
    if (family === undefined) {
      families.set(key, {
        kind: 'family',
        key,
        prefix: familyPrefix(label.name) ?? label.name,
        labels: [label],
        photoCount: label.photo_count,
        expanded: open.includes(key),
      })
      continue
    }
    family.labels.push(label)
    family.photoCount += label.photo_count
  }

  for (const family of families.values()) {
    if (family.labels.length < LABEL_FAMILY_MIN) {
      entries.push(...family.labels.map(asChip))
    } else {
      entries.push(family)
    }
  }

  return entries
}
