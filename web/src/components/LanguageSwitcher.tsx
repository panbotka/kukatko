import Button from 'react-bootstrap/Button'
import ButtonGroup from 'react-bootstrap/ButtonGroup'
import { useTranslation } from 'react-i18next'

import { supportedLngs } from '../i18n'

const LABEL_KEYS = {
  cs: 'language.cs',
  en: 'language.en',
} as const

/**
 * A compact button group that switches the active UI language. The chosen
 * language is persisted by i18next's language detector (localStorage); with
 * nothing stored the app falls back to Czech.
 *
 * It lives in the language section of the account page — every user of this
 * instance is Czech, so the setting does not earn a permanent seat in the navbar.
 */
export function LanguageSwitcher() {
  const { i18n, t } = useTranslation()
  const active = i18n.resolvedLanguage ?? i18n.language

  return (
    <ButtonGroup size="sm" aria-label={t('language.switch')}>
      {supportedLngs.map((lng) => {
        const isActive = active === lng
        return (
          <Button
            key={lng}
            variant={isActive ? 'light' : 'outline-light'}
            active={isActive}
            aria-pressed={isActive}
            onClick={() => {
              void i18n.changeLanguage(lng)
            }}
          >
            {t(LABEL_KEYS[lng])}
          </Button>
        )
      })}
    </ButtonGroup>
  )
}
