import { createInstance } from 'i18next'
import LanguageDetector from 'i18next-browser-languagedetector'
import { beforeEach, describe, expect, it } from 'vitest'

import csCommon from './locales/cs/common.json'
import enCommon from './locales/en/common.json'
import { initOptions, supportedLngs } from './index'

/** The localStorage key i18next-browser-languagedetector caches under. */
const STORAGE_KEY = 'i18nextLng'

/** Boots a throwaway i18next on the app's own options, as a first load would. */
async function bootFreshInstance() {
  const instance = createInstance()
  await instance.use(LanguageDetector).init(initOptions)
  return instance
}

/** A nested i18next resource tree: strings, or further nested objects. */
interface ResourceTree {
  [key: string]: string | ResourceTree
}

/** The CLDR plural-category suffixes i18next appends to pluralized keys. */
const PLURAL_SUFFIXES = ['zero', 'one', 'two', 'few', 'many', 'other'] as const
const PLURAL_SUFFIX_RE = /_(zero|one|two|few|many|other)$/

/**
 * Flattens a nested resource tree into dot-separated leaf keys mapped to their
 * string values, e.g. `{ a: { b: "x" } }` → `{ "a.b": "x" }`.
 */
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

/** Strips a trailing CLDR plural suffix, yielding the language-agnostic key. */
function logicalKey(key: string): string {
  return key.replace(PLURAL_SUFFIX_RE, '')
}

/** Returns the `{{var}}` interpolation names referenced by a translation value. */
function placeholders(value: string): Set<string> {
  const names = new Set<string>()
  for (const match of value.matchAll(/\{\{\s*([\w-]+)\s*\}\}/g)) {
    names.add(match[1])
  }
  return names
}

const locales = {
  cs: flatten(csCommon),
  en: flatten(enCommon),
}

/**
 * These tests are the drift guard for the bilingual UI: they fail if the Czech
 * and English resource files disagree on which keys exist, leave a value empty,
 * forget a Czech plural category, or drop an interpolation variable in one
 * language. Keeping cs/en in lockstep prevents missing-translation warnings from
 * sneaking in across feature work.
 */
describe('i18n resource parity', () => {
  it('exposes exactly Czech and English as supported languages', () => {
    expect([...supportedLngs].sort()).toEqual(Object.keys(locales).sort())
  })

  it('has identical logical key sets across cs and en', () => {
    const csKeys = new Set(Object.keys(locales.cs).map(logicalKey))
    const enKeys = new Set(Object.keys(locales.en).map(logicalKey))

    const onlyCs = [...csKeys].filter((k) => !enKeys.has(k)).sort()
    const onlyEn = [...enKeys].filter((k) => !csKeys.has(k)).sort()

    expect(onlyCs, 'keys present only in cs').toEqual([])
    expect(onlyEn, 'keys present only in en').toEqual([])
  })

  it('has no empty or whitespace-only values in any language', () => {
    for (const [lng, flat] of Object.entries(locales)) {
      const empty = Object.entries(flat)
        .filter(([, value]) => value.trim() === '')
        .map(([key]) => key)
      expect(empty, `empty values in ${lng}`).toEqual([])
    }
  })

  it('provides every CLDR plural category each language requires', () => {
    for (const [lng, flat] of Object.entries(locales)) {
      // The plural categories the language genuinely uses for cardinals.
      const required = new Set(
        // 0..20 covers one/few/many/other for cs and one/other for en.
        Array.from({ length: 21 }, (_v, n) => new Intl.PluralRules(lng).select(n)),
      )
      // Group the suffixed keys by their logical base.
      const variants = new Map<string, Set<string>>()
      for (const key of Object.keys(flat)) {
        const match = PLURAL_SUFFIX_RE.exec(key)
        if (!match) continue
        const base = logicalKey(key)
        const set = variants.get(base) ?? new Set<string>()
        set.add(match[1])
        variants.set(base, set)
      }
      for (const [base, present] of variants) {
        const missing = [...required].filter((cat) => !present.has(cat)).sort()
        expect(missing, `${lng}: ${base} is missing plural categories`).toEqual([])
        const extra = [...present].filter(
          (cat) => !(PLURAL_SUFFIXES as readonly string[]).includes(cat),
        )
        expect(extra, `${lng}: ${base} has unknown plural suffixes`).toEqual([])
      }
    }
  })

  it('uses the same interpolation variables for each logical key in both languages', () => {
    // Aggregate the placeholders used by every variant of a logical key.
    function placeholdersByLogicalKey(flat: Record<string, string>): Map<string, Set<string>> {
      const map = new Map<string, Set<string>>()
      for (const [key, value] of Object.entries(flat)) {
        const base = logicalKey(key)
        const set = map.get(base) ?? new Set<string>()
        for (const name of placeholders(value)) set.add(name)
        map.set(base, set)
      }
      return map
    }

    const csVars = placeholdersByLogicalKey(locales.cs)
    const enVars = placeholdersByLogicalKey(locales.en)
    const mismatches: string[] = []
    for (const [key, csSet] of csVars) {
      const enSet = enVars.get(key) ?? new Set<string>()
      const same = csSet.size === enSet.size && [...csSet].every((v) => enSet.has(v))
      if (!same) {
        mismatches.push(
          `${key}: cs={${[...csSet].sort().join(',')}} en={${[...enSet].sort().join(',')}}`,
        )
      }
    }
    expect(mismatches).toEqual([])
  })
})

/**
 * Czech is the default of this instance, not merely its fallback string table:
 * a brand-new visitor with no stored preference must land on Czech regardless of
 * what their browser asks for. The language switcher (account page) is the only
 * thing that changes it, and it persists through localStorage.
 */
describe('default language', () => {
  beforeEach(() => {
    window.localStorage.removeItem(STORAGE_KEY)
  })

  it('resolves to Czech on a first visit, whatever the browser prefers', async () => {
    // Precondition: jsdom reports an English browser, so a `navigator` detector
    // would win here. Without one, the Czech `fallbackLng` decides.
    expect(navigator.language.startsWith('cs')).toBe(false)

    const instance = await bootFreshInstance()

    expect(instance.resolvedLanguage).toBe('cs')
  })

  it('restores a stored preference over the default', async () => {
    window.localStorage.setItem(STORAGE_KEY, 'en')

    const instance = await bootFreshInstance()

    expect(instance.resolvedLanguage).toBe('en')
  })

  it('persists the chosen language so the next visit reopens in it', async () => {
    const instance = await bootFreshInstance()
    await instance.changeLanguage('en')

    expect(window.localStorage.getItem(STORAGE_KEY)).toBe('en')
  })
})
