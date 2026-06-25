import i18n from 'i18next'
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

// Fire-and-forget init: i18next resolves synchronously for bundled resources,
// so the app can render immediately while react-i18next subscribes to changes.
void i18n
  .use(LanguageDetector)
  .use(initReactI18next)
  .init({
    resources,
    fallbackLng: 'cs',
    supportedLngs: [...supportedLngs],
    defaultNS,
    ns: [defaultNS],
    detection: {
      order: ['localStorage', 'navigator', 'htmlTag'],
      caches: ['localStorage'],
    },
    interpolation: {
      escapeValue: false,
    },
    react: {
      useSuspense: false,
    },
  })

export default i18n
