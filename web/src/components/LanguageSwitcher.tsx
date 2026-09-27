import { useContext } from 'react'
import Button from 'react-bootstrap/Button'
import ButtonGroup from 'react-bootstrap/ButtonGroup'
import { useTranslation } from 'react-i18next'

import { AuthContext } from '../auth/AuthContext'
import { supportedLngs } from '../i18n'
import { writeLanguagePreference } from '../i18n/accountLanguage'

const LABEL_KEYS = {
  cs: 'language.cs',
  en: 'language.en',
} as const

/**
 * A compact button group that switches the active UI language. The choice is
 * the signed-in account's: it is remembered in this browser under the account's
 * uid (see {@link writeLanguagePreference}) and applied again whenever that
 * account signs in, while anybody else on the same browser keeps their own —
 * Czech until they choose.
 *
 * It lives in the language section of the account page — every user of this
 * instance is Czech, so the setting does not earn a permanent seat in the navbar.
 */
export function LanguageSwitcher() {
  const { i18n, t } = useTranslation()
  // Read null-safely: outside a session the switch still applies, it is just
  // not remembered for anybody.
  const user = useContext(AuthContext)?.user?.uid
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
              writeLanguagePreference(user, lng)
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
