import Badge from 'react-bootstrap/Badge'
import { useTranslation } from 'react-i18next'

/**
 * The count of tasks waiting on the reader, as a small pill beside a nav label
 * or over the hamburger. Renders nothing for zero — a badge is a summons, and a
 * "0" summons nobody — so callers can drop it in unconditionally.
 *
 * The number is what a sighted reader sees; a screen reader gets the sentence
 * instead ("3 úkoly čekají na tebe"), because a bare digit read out after
 * „Úkoly" would be noise. The `title` repeats the sentence for a hover.
 *
 * `over` places it on the corner of its (relatively positioned) parent — the
 * hamburger — instead of inline after a label.
 */
export function WaitingBadge({ count, over = false }: { count: number; over?: boolean }) {
  const { t } = useTranslation()
  if (count <= 0) {
    return null
  }
  const label = t('tasks.waitingBadge', { count })
  return (
    <Badge
      pill
      bg="danger"
      className={`kk-waiting-badge${over ? ' kk-waiting-badge--over' : ''}`}
      title={label}
      data-testid="waiting-badge"
    >
      <span aria-hidden="true">{count}</span>
      <span className="visually-hidden">{label}</span>
    </Badge>
  )
}
