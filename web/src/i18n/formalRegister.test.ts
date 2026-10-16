import { describe, expect, it } from 'vitest'

import csCommon from './locales/cs/common.json'

/** A nested i18next resource tree: strings, or further nested objects. */
interface ResourceTree {
  [key: string]: string | ResourceTree
}

/** Flattens a nested resource tree into dot-separated leaf keys → values. */
function flatten(tree: ResourceTree, prefix = '', out: Record<string, string> = {}) {
  for (const [key, value] of Object.entries(tree)) {
    const path = prefix ? `${prefix}.${key}` : key
    if (typeof value === 'string') {
      out[path] = value
    } else {
      flatten(value, path, out)
    }
  }
  return out
}

/**
 * Pronouns and second-person-singular verbs that only the informal register
 * (tykání) uses. "ty" is deliberately missing: it is also the plural
 * demonstrative ("i ty, které…"), so it would flag good Czech.
 */
const INFORMAL_WORDS = [
  'tě',
  'tebe',
  'tobě',
  'ti',
  'tvůj',
  'tvoje',
  'tvé',
  'tvého',
  'tvému',
  'tvým',
  'tvou',
  'jsi',
  'nejsi',
  'máš',
  'můžeš',
  'chceš',
  // Frequent singular imperatives that end in neither "-ej", "-ni" nor "-ij",
  // so the leading-imperative rule below cannot see them.
  'napiš',
  'piš',
  'vyber',
  'zkus',
  'ulož',
  'uprav',
  'nastav',
  'označ',
  'potvrď',
  'spusť',
  'zvol',
  'vytvoř',
  'založ',
  'odeber',
  'připoj',
  'zruš',
  'doplň',
  'vyplň',
  'nech',
  'pošli',
]

/** Any of {@link INFORMAL_WORDS} as a whole word (letters on neither side). */
const INFORMAL_WORD_RE = new RegExp(`(?<!\\p{L})(${INFORMAL_WORDS.join('|')})(?!\\p{L})`, 'iu')

/**
 * A second-person-singular present verb: "rozhoduješ", "najdeš", "víš",
 * "nepotvrdíš". The formal plural ends in "-te" instead.
 */
const SINGULAR_VERB_RE = /(?<!\p{L})(\p{L}+(?:eš|íš|áš))(?!\p{L})/giu

/**
 * A string that opens with a singular imperative: "Zadej…", "Klikni…",
 * "Pojmenuj…", "Použij…". The formal plural always ends in "-te" ("Zadejte",
 * "Klikněte", "Použijte"), so the singular endings never match it.
 */
const LEADING_IMPERATIVE_RE = /^(\p{L}*(?:ej|uj|ni|ij))(?!\p{L})/iu

/**
 * Genuine false positives: words the rules above catch that are not tykání,
 * each with its reason. A word listed here that no Czech string trips over any
 * more fails the suite, so the list cannot rot.
 */
const ALLOWED_WORDS: Record<string, string> = {
  váš: 'the formal possessive itself ("váš účet"), which merely ends in "-áš"',
  nejspíš: 'the adverb "most likely", not a verb',
  obličej: 'the noun "face", which merely ends in "-ej"',
  všichni: 'the pronoun "everybody", which merely ends in "-ni"',
}

/** Drops `{{placeholder}}` names, which are code, not Czech. */
function prose(value: string): string {
  return value.replace(/\{\{[^}]*\}\}/g, ' ')
}

/** Whether a matched word is a listed false positive. */
function allowed(word: string): boolean {
  return word.toLowerCase() in ALLOWED_WORDS
}

/**
 * Why a Czech value reads as tykání, or null when it reads formally. Every
 * allow-listed word it skipped is added to `skipped`, when given.
 */
function informalMarker(value: string, skipped?: Set<string>): string | null {
  const text = prose(value).trim()
  const word = INFORMAL_WORD_RE.exec(text)
  if (word) {
    return `informal word "${word[1]}"`
  }
  for (const verb of text.matchAll(SINGULAR_VERB_RE)) {
    if (!allowed(verb[1])) {
      return `second-person singular "${verb[1]}"`
    }
    skipped?.add(verb[1].toLowerCase())
  }
  const imperative = LEADING_IMPERATIVE_RE.exec(text)
  if (imperative) {
    if (!allowed(imperative[1])) {
      return `singular imperative "${imperative[1]}"`
    }
    skipped?.add(imperative[1].toLowerCase())
  }
  return null
}

const cs = flatten(csCommon)

/**
 * The Czech UI addresses the reader formally (vykání), the same as every
 * e-mail Kukátko sends. This guard fails on the common markers of the informal
 * register so that a new string cannot quietly bring tykání back.
 */
describe('Czech formal register (vykání)', () => {
  it('has no informal marker in any Czech string', () => {
    const offenders = Object.entries(cs)
      .map(([key, value]) => [key, informalMarker(value)] as const)
      .filter(([, marker]) => marker !== null)
      .map(([key, marker]) => `${key}: ${marker}`)
    expect(offenders).toEqual([])
  })

  it('allow-lists only words some Czech string still trips over', () => {
    const skipped = new Set<string>()
    for (const value of Object.values(cs)) {
      informalMarker(value, skipped)
    }
    const stale = Object.keys(ALLOWED_WORDS).filter((word) => !skipped.has(word))
    expect(stale).toEqual([])
  })

  it.each([
    ['Napiš komentář…', 'informal word "Napiš"'],
    ['Zadej jméno', 'singular imperative'],
    ['Klikni na Hledat', 'singular imperative'],
    ['Pojmenuj celou skupinu', 'singular imperative'],
    ['Vítej v Kukátku', 'singular imperative'],
    ['Vyber osobu a spusť hledání', 'informal word "Vyber"'],
    ['Rozhoduje se teď, rozhoduješ ty', 'second-person singular "rozhoduješ"'],
    ['Použij desetinné stupně', 'singular imperative'],
    ['{{name}} tě zmínil(a) v komentáři', 'informal word "tě"'],
    ['3 úkoly čekají na tebe', 'informal word "tebe"'],
    ['Rozhoduješ ty — chceš?', 'informal word "chceš"'],
    ['Jsi offline.', 'informal word "Jsi"'],
  ])('flags the informal %j', (value, marker) => {
    expect(informalMarker(value)).toContain(marker)
  })

  it.each([
    'Napište komentář…',
    'Zadejte jméno',
    'Klikněte na Hledat',
    '{{name}} vás zmínil(a) v komentáři',
    'Záznamy všech projektů i ty, které k žádnému nepatří',
    'Jste offline.',
    'Tiché přehrávání',
    'Nejspíš byl smazán. Příliš mnoho fotek v koši.',
    '{{count}} obličej',
    'Rozhodujete vy — vaše volba.',
  ])('passes the formal %j', (value) => {
    expect(informalMarker(value)).toBeNull()
  })
})
