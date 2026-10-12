import { type ReactNode, useState } from 'react'
import Button from 'react-bootstrap/Button'
import Dropdown from 'react-bootstrap/Dropdown'
import { useTranslation } from 'react-i18next'

import { useIsNarrowViewport } from '../hooks/useIsNarrowViewport'
import { ProjectLink } from '../project/ProjectLink'

import { Icon, type IconName } from './Icon'

/**
 * One action of a page header that may end up in the "…" menu. It is described
 * rather than rendered, because the same action is a button on the header row
 * of a wide screen and a menu item (`Dropdown.Item`, `role="menuitem"`) on a
 * phone — and a ready-made button dropped into a menu is a stack of outlined
 * blocks the arrow keys cannot walk.
 */
export interface HeaderAction {
  /** Stable React key, unique within the group. */
  key: string
  /** The visible label. */
  label: ReactNode
  /** An optional leading glyph. */
  icon?: IconName
  /** Runs the action. Ignored when `to` is set. */
  onSelect?: () => void
  /**
   * An in-app address — a page's own path, opened in the current project: the
   * action is a link (openable in a new tab, copyable) rather than a button.
   */
  to?: string
  disabled?: boolean
  /**
   * Bootstrap variant of the action when it stands on the header row (a
   * secondary action on a wide screen); a menu item is always a plain item.
   * Defaults to `outline-secondary`.
   */
  inlineVariant?: string
}

/**
 * A slot of an action list: an action, or what a conditionally rendered one
 * (`canCurate && {…}`) evaluates to when it is not there.
 */
export type HeaderActionSlot = HeaderAction | false | null | undefined

/** Props for {@link HeaderActions}. */
export interface HeaderActionsProps {
  /**
   * The one or two actions that stay directly reachable at every width. Keep the
   * list short — everything here has to fit beside the page title on a phone.
   * They never enter the menu, so they are passed as ready-made nodes.
   */
  primary?: readonly ReactNode[]
  /**
   * Secondary actions: inline buttons on desktop, folded into the "…" overflow
   * menu on a phone (where they lead the menu).
   */
  secondary?: readonly HeaderActionSlot[]
  /**
   * Occasional actions that live in the "…" overflow menu **at every width**, so
   * a desktop header shows only its one or two real actions instead of a row of
   * equal buttons nothing stands out from.
   */
  overflow?: readonly HeaderActionSlot[]
  /**
   * Destructive actions (delete and friends): always in the "…" overflow menu,
   * at every width, last, in the danger colour and behind a divider so a mis-tap
   * cannot land on them from the neutral actions above.
   */
  destructive?: readonly HeaderActionSlot[]
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
function present<T>(nodes: readonly (T | false | null | undefined)[] | undefined): T[] {
  return (nodes ?? []).filter(
    (node): node is T => node !== null && node !== undefined && node !== false,
  )
}

/** The label with its optional glyph in front. */
function content(action: HeaderAction): ReactNode {
  return (
    <>
      {action.icon !== undefined && <Icon name={action.icon} className="me-2" />}
      {action.label}
    </>
  )
}

/** A secondary action standing on the header row of a wide screen. */
function InlineAction({ action }: { action: HeaderAction }) {
  const variant = action.inlineVariant ?? 'outline-secondary'
  if (action.to !== undefined) {
    return (
      <ProjectLink to={action.to} className={`btn btn-${variant} btn-sm`}>
        {content(action)}
      </ProjectLink>
    )
  }
  return (
    <Button variant={variant} size="sm" disabled={action.disabled} onClick={action.onSelect}>
      {content(action)}
    </Button>
  )
}

/** An action as an item of the "…" menu. */
function MenuAction({ action, danger }: { action: HeaderAction; danger?: boolean }) {
  const className = danger === true ? 'text-danger' : undefined
  if (action.to !== undefined) {
    return (
      <Dropdown.Item as={ProjectLink} to={action.to} role="menuitem" className={className}>
        {content(action)}
      </Dropdown.Item>
    )
  }
  return (
    <Dropdown.Item
      as="button"
      type="button"
      role="menuitem"
      className={className}
      disabled={action.disabled}
      onClick={action.onSelect}
    >
      {content(action)}
    </Dropdown.Item>
  )
}

/**
 * The action group of a page header: the primary actions always inline, the
 * secondary ones inline on desktop, and everything else — occasional actions
 * and, last, the destructive ones — in a single "…" overflow `Dropdown`. On a
 * phone (`useIsNarrowViewport`) the secondary actions fold into that menu too,
 * so the header keeps to one compact row instead of wrapping into a two- or
 * three-row block of 44px targets.
 *
 * Everything that may enter the menu is a {@link HeaderAction} descriptor (in
 * an array, so a conditionally rendered `false` can be told apart from a real
 * action): the group renders it as a button on the row or as a proper menu
 * item — `role="menuitem"`, walked with the arrow keys, closing the menu when
 * chosen — while the page keeps its RBAC gating and its handlers where they
 * already were.
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
      {inlineItems.map((action) => (
        <InlineAction key={action.key} action={action} />
      ))}
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
          <Dropdown.Menu className="kk-header-overflow-menu" role="menu" aria-labelledby={id}>
            {menuItems.map((action) => (
              <MenuAction key={action.key} action={action} />
            ))}
            {menuItems.length > 0 && destructiveItems.length > 0 && <Dropdown.Divider />}
            {destructiveItems.map((action) => (
              <MenuAction key={action.key} action={action} danger />
            ))}
          </Dropdown.Menu>
        </Dropdown>
      )}
    </div>
  )
}
