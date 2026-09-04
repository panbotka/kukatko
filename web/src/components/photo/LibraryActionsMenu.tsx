import Dropdown from 'react-bootstrap/Dropdown'
import { useTranslation } from 'react-i18next'

import { Icon } from '../Icon'

/** Props for {@link LibraryActionsMenu}. */
export interface LibraryActionsMenuProps {
  /** Whether the photo currently sits in the trash. */
  archived: boolean
  /** Whether the photo is currently held back from the library. */
  hidden: boolean
  /** True while the archive/restore call is in flight, so its item cannot re-fire. */
  archivePending: boolean
  /** True while the hide/show call is in flight, so its item cannot re-fire. */
  hidePending: boolean
  /** Archives the photo, or restores it from the trash. */
  onToggleArchive: () => void
  /** Hides the photo from the library, or brings it back. */
  onToggleHidden: () => void
  /** Whether the menu is open — owned by the viewer, which pins its chrome while it is. */
  open: boolean
  /** Reports every open/close, including a click outside and Escape. */
  onOpenChange: (open: boolean) => void
}

/**
 * The viewer's library operations — archive/restore and hide/show — behind one
 * overflow control, **as text, never as a bare glyph**.
 *
 * These two are the only controls in the viewer that change *what is in the
 * library*: everything else beside them either changes the view (faces, edits,
 * info) or records this reader's own opinion (stars, marks, the heart). Presented
 * as peers in the same row of identical round icons they were unreadable — the
 * archive box names a flag no glyph exists for, and hidden/shown is an eye versus
 * a struck-through eye, a hairline apart at 1rem. So they leave the row: the
 * consequence is written out („Skrýt z knihovny", „Vrátit z koše"), which is the
 * only presentation in which an act that removes a photograph from the library
 * can be *read* rather than guessed.
 *
 * The label carries the state as well as the act, because it names the act that
 * is available *now* — „Skrýt z knihovny" can only be offered to a visible photo.
 * {@link import('./PhotoFlagBadges').PhotoFlagBadges} repeats the state in words
 * under the photo's title, which is where a viewer (who gets no menu at all)
 * reads it.
 *
 * The open state is lifted to the viewer, which pins the auto-hiding chrome while
 * the menu is up: a menu whose trigger melts away under the reader's hand is a
 * menu that cannot be closed by the control that opened it.
 */
export function LibraryActionsMenu({
  archived,
  hidden,
  archivePending,
  hidePending,
  onToggleArchive,
  onToggleHidden,
  open,
  onOpenChange,
}: LibraryActionsMenuProps) {
  const { t } = useTranslation()
  const archiveLabel = archived ? t('photo.archive.restore') : t('batch.archive')
  const hideLabel = hidden ? t('photo.hidden.show') : t('photo.hidden.hide')

  return (
    <Dropdown align="end" show={open} onToggle={onOpenChange} className="kk-viewer__group">
      <Dropdown.Toggle
        as="button"
        type="button"
        id="viewer-library-actions"
        className="kk-viewer__btn kk-viewer__btn--icon kk-viewer__library-toggle"
        aria-label={t('photo.viewer.library')}
        title={t('photo.viewer.library')}
      >
        <Icon name="three-dots" />
      </Dropdown.Toggle>
      <Dropdown.Menu className="kk-viewer__library-menu">
        {/* The heading says what this handful of items has in common — that they
            change the library itself — so the group is named where it is read,
            not only in the trigger's tooltip. */}
        <Dropdown.Header>{t('photo.viewer.libraryGroup')}</Dropdown.Header>
        <Dropdown.Item
          as="button"
          type="button"
          disabled={hidePending}
          // A flag you cannot list is a flag you cannot undo, so the item that
          // sets it also names the search that finds the photo again.
          title={hidden ? t('photo.hidden.showHint') : t('photo.hidden.hideHint')}
          onClick={onToggleHidden}
        >
          <Icon name={hidden ? 'eye-slash' : 'eye'} className="me-2" />
          {hideLabel}
        </Dropdown.Item>
        <Dropdown.Item
          as="button"
          type="button"
          disabled={archivePending}
          title={archiveLabel}
          onClick={onToggleArchive}
        >
          <Icon name="archive" className="me-2" />
          {archiveLabel}
        </Dropdown.Item>
      </Dropdown.Menu>
    </Dropdown>
  )
}
