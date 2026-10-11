import type { ReactNode } from 'react'
import Container from 'react-bootstrap/Container'
import { useTranslation } from 'react-i18next'

import { Icon } from './Icon'

/** Public home of the project's source code, linked from every page's footer. */
const GITHUB_URL = 'https://github.com/panbotka/kukatko'

/** Props for {@link Footer}. */
interface FooterProps {
  /** Optional status content for the right-hand side of the footer row. */
  children?: ReactNode
}

/**
 * Global page footer: names who operates this instance and links to the
 * project's source code on GitHub. It renders in normal document flow below
 * the routed content — on short pages it simply follows the content rather
 * than floating over it or sticking to the viewport bottom. The footer is a
 * space-between flex row with the operator info on the left; `children` fill
 * the right-hand side (e.g. the maintainers' job-queue status line) without restructuring
 * it. Its `display: flex` is the stylesheet's (`.kukatko-footer`), not a
 * `d-flex` utility, whose `!important` would keep it on the one page that drops
 * it — the full-viewport map (`.kk-map-fill`).
 */
export function Footer({ children }: FooterProps) {
  const { t } = useTranslation()

  return (
    <Container
      as="footer"
      className="kukatko-footer flex-wrap justify-content-between align-items-center gap-2 border-top py-3 small text-body-secondary"
    >
      <span>
        {t('footer.operator')}
        {' · '}
        <a
          href={GITHUB_URL}
          target="_blank"
          rel="noopener noreferrer"
          title={t('footer.githubTitle')}
          className="link-secondary"
        >
          <Icon name="github" className="me-1" />
          {t('footer.github')}
        </a>
      </span>
      {/* The right-hand side of the space-between row hosts optional status
          content (the maintainers' job-queue status line); empty for everyone else. */}
      {children}
    </Container>
  )
}
