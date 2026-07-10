import Container from 'react-bootstrap/Container'
import { useTranslation } from 'react-i18next'

import { Icon } from './Icon'

/** Public home of the project's source code, linked from every page's footer. */
const GITHUB_URL = 'https://github.com/panbotka/kukatko'

/**
 * Global page footer: names who operates this instance and links to the
 * project's source code on GitHub. It renders in normal document flow below
 * the routed content — on short pages it simply follows the content rather
 * than floating over it or sticking to the viewport bottom. The footer is a
 * space-between flex row with the operator info on the left, so a small
 * status area can later occupy the right-hand side without restructuring it.
 */
export function Footer() {
  const { t } = useTranslation()

  return (
    <Container
      as="footer"
      className="kukatko-footer d-flex flex-wrap justify-content-between align-items-center gap-2 border-top py-3 small text-body-secondary"
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
      {/* The right-hand side of the space-between row is intentionally empty:
          it is reserved for a small status area to land in later. */}
    </Container>
  )
}
