import i18n, { type i18n as I18n, type InitOptions } from 'i18next'
import LanguageDetector from 'i18next-browser-languagedetector'
import { initReactI18next } from 'react-i18next'

import csCommon from './locales/cs/common.json'
import enCommon from './locales/en/common.json'

export const defaultNS = 'common'

/** Supported UI languages, with Czech as the default per project convention. */
export const supportedLngs = ['cs', 'en'] as const

export const resources = {
  cs: { common: csCommon },
  en: { common: enCommon },
} as const

/**
 * The i18next configuration, exported so tests can init a throwaway instance
 * with the exact options the app runs on.
 *
 * The only detector is `localStorage`, which the language switcher on the
 * account page writes to. Reading `navigator`/`htmlTag` too would hand an
 * English-locale browser an English UI on first visit; this instance is Czech,
 * so with no stored preference the `fallbackLng` decides and Czech wins.
 */
export const initOptions: InitOptions = {
  resources,
  // i18next prints a console.info ad for Locize on every init unless told not
  // to. Silenced in every build, dev included: a console with nothing in it is
  // one where a real warning is visible at a glance.
  showSupportNotice: false,
  fallbackLng: 'cs',
  supportedLngs: [...supportedLngs],
  defaultNS,
  ns: [defaultNS],
  detection: {
    order: ['localStorage'],
    caches: ['localStorage'],
  },
  interpolation: {
    escapeValue: false,
  },
  react: {
    useSuspense: false,
  },
}

/**
 * Keeps `<html lang>` on the language the UI is actually in. index.html ships
 * `cs`, which is right only until someone picks English; a stale attribute
 * makes a screen reader read English text with Czech pronunciation.
 *
 * Subscribes to `languageChanged`, which i18next emits for the initial
 * resolution inside `init` as well as for every later switch, so calling this
 * before `init` covers first paint too. The resolved language wins over the
 * raw one: a stored `en-US` still renders the English resources and must be
 * announced as `en`. Returns the unsubscribe function.
 */
export function syncDocumentLang(instance: I18n, root: HTMLElement = document.documentElement) {
  const apply = (lng: string) => {
    const lang = instance.resolvedLanguage ?? lng
    if (lang) root.lang = lang
  }
  instance.on('languageChanged', apply)
  if (instance.isInitialized) apply(instance.language)
  return () => {
    instance.off('languageChanged', apply)
  }
}

syncDocumentLang(i18n)

// Fire-and-forget init: i18next resolves synchronously for bundled resources,
// so the app can render immediately while react-i18next subscribes to changes.
void i18n.use(LanguageDetector).use(initReactI18next).init(initOptions)

export default i18n
