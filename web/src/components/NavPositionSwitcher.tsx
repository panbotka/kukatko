import Button from 'react-bootstrap/Button'
import ButtonGroup from 'react-bootstrap/ButtonGroup'
import { useTranslation } from 'react-i18next'

import { NAV_POSITIONS, useNavPosition } from '../lib/navPosition'

const LABEL_KEYS = {
  top: 'account.navPositionTop',
  left: 'account.navPositionLeft',
} as const

/**
 * The account page's "Umístění menu: nahoře / vlevo" switch: where the desktop
 * navigation stands — the top bar (the default) or a sidebar on the left. The
 * choice is the signed-in account's, remembered in this browser under its uid
 * (`lib/navPosition.ts`), and the shell moves the moment it is clicked — no
 * reload. A phone keeps its tab bar and drawer whatever is chosen here.
 */
export function NavPositionSwitcher() {
  const { t } = useTranslation()
  const [position, setPosition] = useNavPosition()

  return (
    <ButtonGroup size="sm" aria-label={t('account.navPosition')}>
      {NAV_POSITIONS.map((option) => {
        const isActive = position === option
        return (
          <Button
            key={option}
            variant="outline-secondary"
            active={isActive}
            aria-pressed={isActive}
            onClick={() => {
              setPosition(option)
            }}
          >
            {t(LABEL_KEYS[option])}
          </Button>
        )
      })}
    </ButtonGroup>
  )
}
