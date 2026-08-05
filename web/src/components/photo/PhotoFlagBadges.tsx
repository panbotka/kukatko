import { useTranslation } from 'react-i18next'

import { Icon } from '../Icon'

/** Props for {@link PhotoFlagBadges}. */
export interface PhotoFlagBadgesProps {
  /** The photo is hidden from the library (`hidden_from_library`). */
  hidden: boolean
  /** The photo sits in the trash (`archived_at` is set). */
  archived: boolean
}

/**
 * The "held back from the library" strip of the viewer: one `danger` pill per
 * flag the open photo carries — hidden from the library, in the trash — right
 * under the title.
 *
 * It exists because a state that lives only on a toggle button is a state you
 * cannot read: the two eye glyphs of Skrýt/Vrátit differ by a hairline at this
 * size, and colour alone would be lost on a colour-blind reader and in every
 * forced-colours mode. The badge says the same thing in words, in a second
 * place, so the flag is legible from the title area without hunting for the
 * control that set it — and it shows for a viewer too, who never gets that
 * control at all.
 *
 * Purely informative: it carries no buttons — setting and clearing the flags
 * stays on the curation controls. Renders nothing for an ordinary photo.
 */
export function PhotoFlagBadges({ hidden, archived }: PhotoFlagBadgesProps) {
  const { t } = useTranslation()

  if (!hidden && !archived) {
    return null
  }

  // Not `text-bg-danger`: white on the bare `--bs-danger` measures 3.96:1, under
  // AA for text this small. `kk-viewer__flag-badge` deepens the same token until
  // white clears it, exactly as the toggle's on-state does.
  const pill = 'badge rounded-pill kk-viewer__flag-badge d-inline-flex align-items-center gap-1'

  return (
    <div className="kk-viewer__flags">
      {archived && (
        <span className={pill}>
          <Icon name="archive" />
          {t('photo.archive.badge')}
        </span>
      )}
      {hidden && (
        <span className={pill}>
          <Icon name="eye-slash" />
          {t('photo.hidden.badge')}
        </span>
      )}
    </div>
  )
}
