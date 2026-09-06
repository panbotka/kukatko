import i18n, { type InitOptions } from 'i18next'
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

// Fire-and-forget init: i18next resolves synchronously for bundled resources,
// so the app can render immediately while react-i18next subscribes to changes.
void i18n.use(LanguageDetector).use(initReactI18next).init(initOptions)

export default i18n
