import type { ReactNode } from 'react'
import Card from 'react-bootstrap/Card'
import { useTranslation } from 'react-i18next'

/** How wide the card may grow: sign-in is two fields, the other forms carry more. */
export type AuthFrameWidth = 'narrow' | 'wide'

interface AuthFrameProps {
  /** The card's heading — the route's one `<h1>` ("Přihlášení", "Registrace", …). */
  title: string
  /** `narrow` for sign-in, `wide` (the default) for registration and password reset. */
  width?: AuthFrameWidth
  /** Everything under the heading: alerts, the form, the links below it. */
  children: ReactNode
}

/**
 * The peephole mark beside the wordmark, drawn inline so the frame needs no
 * asset of its own: the housing ring and the aperture take the wordmark's
 * colour (`currentColor`), the glint on the glass the page tone. Decorative —
 * the name next to it already says what it is.
 */
function PeepholeMark() {
  return (
    <svg className="kk-auth-wordmark-mark" viewBox="0 0 32 32" aria-hidden="true" focusable="false">
      <circle cx="16" cy="16" r="13" fill="none" stroke="currentColor" strokeWidth="3" />
      <circle cx="16" cy="16" r="7" fill="currentColor" />
      <circle className="kk-auth-wordmark-glint" cx="13.5" cy="13.5" r="1.75" />
    </svg>
  )
}

/**
 * The frame the signed-out pages share — sign-in, registration and password
 * reset — so all three look alike: the "Kukátko" wordmark over a card,
 * centred in the viewport on desktops and tablets. On a phone the pair sits a
 * fixed ~12% from the top instead, because centring would move the card every
 * time the on-screen keyboard resized the viewport and push the field being
 * typed into off the screen.
 *
 * The wordmark is the app's name, not a heading: the card's title stays the
 * route's single `<h1>`, so a screen reader still lands on "Přihlášení".
 */
export function AuthFrame({ title, width = 'wide', children }: AuthFrameProps) {
  const { t } = useTranslation()
  return (
    <div className="kk-auth-frame" data-testid="auth-frame">
      <div className={`kk-auth-frame-column kk-auth-frame-column--${width}`}>
        <div className="kk-auth-wordmark kk-display" data-testid="auth-wordmark">
          <PeepholeMark />
          {t('documentTitle.app')}
        </div>
        <Card text="light">
          <Card.Body>
            <Card.Title as="h1" className="kk-page-title mb-4 text-center">
              {title}
            </Card.Title>
            {children}
          </Card.Body>
        </Card>
      </div>
    </div>
  )
}
