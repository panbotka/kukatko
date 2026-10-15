import Badge from 'react-bootstrap/Badge'
import { useTranslation } from 'react-i18next'

import { badgeText } from './bell'

/** Props of {@link ProjectUnreadBadge}. */
export interface ProjectUnreadBadgeProps {
  /** How many unread notifications the project holds for the reader. */
  count: number
  /**
   * What the number counts, for a screen reader and the tooltip; the default
   * is the project's own („2 nepřečtená oznámení"). The switcher's toggle says
   * the count is the *other* projects'.
   */
  label?: string
  /** Extra classes, for where the pill sits. */
  className?: string
}

/**
 * The unread mark on a project in the switcher, the phone drawer and the
 * chooser — the bell's red pill, so the two read as one signal. Nothing is
 * drawn for 0. The numeral is `aria-hidden` and the sentence beside it
 * („2 nepřečtená oznámení") is visually hidden, so the row's accessible name
 * says what the number counts.
 */
export function ProjectUnreadBadge({ count, label, className }: ProjectUnreadBadgeProps) {
  const { t } = useTranslation()
  if (count <= 0) {
    return null
  }
  const text = label ?? t('projectSwitcher.unread', { count })
  return (
    <Badge
      pill
      bg="danger"
      className={`kk-waiting-badge kk-project-unread${className === undefined ? '' : ` ${className}`}`}
      title={text}
      data-testid="project-unread"
    >
      <span aria-hidden="true">{badgeText(count)}</span>
      <span className="visually-hidden">{text}</span>
    </Badge>
  )
}
