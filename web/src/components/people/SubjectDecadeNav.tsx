import { useTranslation } from 'react-i18next'

import { type DecadeSection, formatDecade } from '../../lib/photoDecades'

/** Props for {@link SubjectDecadeNav}. */
export interface SubjectDecadeNavProps {
  /** The gallery's decade sections, in the order they appear on the page. */
  sections: DecadeSection[]
  /**
   * The decade the reader last jumped to (`null` is the undated section,
   * `undefined` is "none yet"), highlighted the way the library rail highlights
   * the month under the grid.
   */
  active?: number | null
  /** Jumps the gallery to a decade's section. */
  onJump: (decade: number | null) => void
}

/**
 * A person's gallery in decades: one tick per decade the loaded photos fall in,
 * with how many sit there, and a jump to that decade's section.
 *
 * A person's page spans a life — the archive itself runs 1905–2026 — so „scroll
 * until the sixties turn up" is the wrong way across it. This is the same
 * gesture the library's timeline rail offers, at the grain a life is actually
 * remembered in, and it wears the rail's clothes on purpose: dim ticks with a
 * short mark, the label at full strength, the current one emphasised. It runs
 * horizontally above the gallery rather than down the viewport edge, because it
 * belongs to one section of a page that has a header and review panels of its
 * own — a fixed rail would lie across all of them.
 *
 * It lists the decades of the photos **loaded so far**; the gallery pages, and a
 * decade nobody has loaded is not somewhere a reader can be sent. Loading more
 * simply grows the list. Renders nothing while there is only one decade to show:
 * a navigation with a single destination is furniture, not a way around.
 */
export function SubjectDecadeNav({ sections, active, onJump }: SubjectDecadeNavProps) {
  const { t } = useTranslation()

  if (sections.length < 2) {
    return null
  }

  return (
    <nav className="kk-decade-nav" aria-label={t('subject.decades.label')}>
      {sections.map((section) => {
        const range = formatDecade(section.decade)
        const label = range ?? t('subject.decades.undated')
        const isActive = active !== undefined && active === section.decade
        return (
          <button
            key={label}
            type="button"
            className={`kk-decade-tick${isActive ? ' active' : ''}`}
            aria-current={isActive ? 'true' : undefined}
            aria-label={t('subject.decades.jumpTo', { decade: label })}
            onClick={() => {
              onJump(section.decade)
            }}
          >
            <span className="kk-decade-tick__mark" aria-hidden="true" />
            <span className="kk-decade-tick__label">{label}</span>
            <span className="kk-decade-tick__count">{section.photos.length}</span>
          </button>
        )
      })}
    </nav>
  )
}
