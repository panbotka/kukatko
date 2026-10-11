import { type ReactNode, useState } from 'react'
import Dropdown from 'react-bootstrap/Dropdown'
import { useTranslation } from 'react-i18next'

import { useIsNarrowViewport } from '../hooks/useIsNarrowViewport'

import { Icon } from './Icon'

/** Props for {@link HeaderActions}. */
export interface HeaderActionsProps {
  /**
   * The one or two actions that stay directly reachable at every width. Keep the
   * list short — everything here has to fit beside the page title on a phone.
   */
  primary?: readonly ReactNode[]
  /**
   * Secondary actions: inline on desktop, folded into the "…" overflow menu on a
   * phone (where they lead the menu).
   */
  secondary?: readonly ReactNode[]
  /**
   * Occasional actions that live in the "…" overflow menu **at every width**, so
   * a desktop header shows only its one or two real actions instead of a row of
   * equal buttons nothing stands out from.
   */
  overflow?: readonly ReactNode[]
  /**
   * Destructive actions (delete and friends): always in the "…" overflow menu,
   * at every width, last and behind a divider so a mis-tap cannot land on them
   * from the neutral actions above.
   */
  destructive?: readonly ReactNode[]
  /**
   * DOM id of the overflow toggle. The default suits a page with one action
   * group; give a second group on the same page an id of its own.
   */
  id?: string
}

/**
 * Drops the slots a caller rendered conditionally: `{canWrite && <Button/>}`
 * yields `false`, and an all-`false` list must not raise an empty menu.
 */
function present(nodes: readonly ReactNode[] | undefined): ReactNode[] {
  return (nodes ?? []).filter((node) => node !== null && node !== undefined && node !== false)
}

/**
 * The action group of a page header: the primary actions always inline, the
 * secondary ones inline on desktop, and everything else — occasional actions
 * and, last, the destructive ones — in a single "…" overflow `Dropdown`. On a
 * phone (`useIsNarrowViewport`) the secondary actions fold into that menu too,
 * so the header keeps to one compact row instead of wrapping into a two- or
 * three-row block of 44px targets.
 *
 * Actions are passed as ready-made nodes (arrays, so a conditionally rendered
 * `false` can be told apart from a real action) — the group only decides *where*
 * they are rendered, never how they look or what they do, which keeps a page's
 * RBAC gating and button styling exactly where it already was. Every entry needs
 * a stable `key`, as any array of React children does.
 *
 * The menu closes itself on any click inside: a plain button raises no
 * `select` event that react-bootstrap would act on, and an overflow menu left
 * standing open behind the modal its item just opened reads as a stuck page.
 */
export function HeaderActions({
  primary,
  secondary,
  overflow,
  destructive,
  id = 'header-overflow',
}: HeaderActionsProps) {
  const { t } = useTranslation()
  const narrow = useIsNarrowViewport()
  const [open, setOpen] = useState(false)

  const primaryItems = present(primary)
  const secondaryItems = present(secondary)
  const destructiveItems = present(destructive)
  const inlineItems = narrow ? [] : secondaryItems
  const menuItems = narrow ? [...secondaryItems, ...present(overflow)] : present(overflow)
  // Nothing to hide (a viewer on an empty album, say) means no toggle: an
  // overflow button that opens an empty menu is worse than no button.
  const hasMenu = menuItems.length + destructiveItems.length > 0

  return (
    <div className="d-flex gap-1 flex-wrap align-items-center">
      {primaryItems}
      {inlineItems}
      {hasMenu && (
        <Dropdown align="end" show={open} onToggle={setOpen}>
          <Dropdown.Toggle
            variant="outline-secondary"
            size="sm"
            id={id}
            className="kk-header-overflow-toggle"
            aria-label={t('headerActions.overflow')}
            title={t('headerActions.overflow')}
          >
            <Icon name="three-dots" />
          </Dropdown.Toggle>
          <Dropdown.Menu
            className="kk-header-overflow-menu"
            onClick={() => {
              setOpen(false)
            }}
          >
            {menuItems.length > 0 && <div className="d-grid gap-1">{menuItems}</div>}
            {destructiveItems.length > 0 && (
              <>
                {menuItems.length > 0 && <Dropdown.Divider />}
                <div className="d-grid gap-1">{destructiveItems}</div>
              </>
            )}
          </Dropdown.Menu>
        </Dropdown>
      )}
    </div>
  )
}
