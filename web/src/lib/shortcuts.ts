/**
 * The keyboard-shortcut registry and small pure helpers shared by
 * {@link useKeyboardShortcuts} (dispatch), the grid/detail pages (behaviour) and
 * the help overlay (discoverable listing). Keeping the registry data-only means
 * the same source of truth documents and drives every shortcut.
 */

import type { ParseKeys } from 'i18next'

/** The key that opens the shortcuts help overlay (`Shift+/`). */
export const HELP_SHORTCUT_KEY = '?'

/**
 * Normalizes a `KeyboardEvent.key` to the token used to look up a handler:
 * single-character keys are lower-cased (so `f` and `Shift+F` both match `f`, and
 * `?` stays `?`), while named keys such as `ArrowUp`/`Enter`/`Escape` pass through
 * unchanged. A pure function so dispatch is trivially testable.
 */
export function shortcutToken(key: string): string {
  return key.length === 1 ? key.toLowerCase() : key
}

/**
 * Reports whether a Bootstrap modal that contains form controls is currently
 * open. Global shortcuts are suppressed while such a dialog is up so typing or
 * tabbing inside a bulk-edit / save-view / rename modal never triggers a
 * grid/detail shortcut behind it. A modal with no form control (like the
 * shortcuts help itself) does not count, so it can still be dismissed normally.
 */
export function isFormModalOpen(root: ParentNode = document): boolean {
  return Array.from(root.querySelectorAll('.modal.show')).some(
    (modal) => modal.querySelector('input, textarea, select, form') !== null,
  )
}

/** One shortcut row in the help overlay: the keys and its i18n description key. */
export interface ShortcutEntry {
  /** Display tokens for the key(s), e.g. `['↑', '↓', '←', '→']`. */
  readonly keys: readonly string[]
  /**
   * i18n key resolving to the human description of what the shortcut does. Typed
   * as {@link ParseKeys} so the registry is checked against the locale files (an
   * unknown key is a compile error) yet `t()` still accepts it directly.
   */
  readonly descriptionKey: ParseKeys
}

/** A context-scoped group of shortcuts (Grid / Detail) for the help overlay. */
export interface ShortcutGroup {
  /** i18n key for the group heading (checked against the locale files). */
  readonly titleKey: ParseKeys
  /** The shortcuts in this context. */
  readonly entries: readonly ShortcutEntry[]
}

/**
 * The canonical, grouped list of shortcuts shown in the help overlay. This is the
 * single source of truth for what the UI advertises; the actual key handling
 * lives in the grid/detail pages but mirrors these entries.
 */
export const SHORTCUT_GROUPS: readonly ShortcutGroup[] = [
  {
    titleKey: 'shortcuts.groups.grid',
    entries: [
      { keys: ['↑', '↓', '←', '→', 'j', 'k', 'h', 'l'], descriptionKey: 'shortcuts.grid.move' },
      { keys: ['Enter'], descriptionKey: 'shortcuts.grid.open' },
      { keys: ['x'], descriptionKey: 'shortcuts.grid.select' },
      { keys: ['f'], descriptionKey: 'shortcuts.grid.favorite' },
      { keys: ['Esc'], descriptionKey: 'shortcuts.grid.escape' },
    ],
  },
  {
    titleKey: 'shortcuts.groups.detail',
    entries: [
      { keys: ['←', '→'], descriptionKey: 'shortcuts.detail.prevNext' },
      { keys: ['f'], descriptionKey: 'shortcuts.detail.favorite' },
      { keys: ['m'], descriptionKey: 'shortcuts.detail.faces' },
      { keys: ['Esc'], descriptionKey: 'shortcuts.detail.back' },
    ],
  },
  {
    titleKey: 'shortcuts.groups.faceSearch',
    entries: [
      {
        keys: ['↑', '↓', '←', '→', 'j', 'k', 'h', 'l'],
        descriptionKey: 'shortcuts.faceSearch.move',
      },
      { keys: ['y', 'Enter'], descriptionKey: 'shortcuts.faceSearch.confirm' },
      { keys: ['n'], descriptionKey: 'shortcuts.faceSearch.reject' },
    ],
  },
  {
    titleKey: 'shortcuts.groups.outliers',
    entries: [
      {
        keys: ['↑', '↓', '←', '→', 'j', 'k', 'h', 'l'],
        descriptionKey: 'shortcuts.outliers.move',
      },
      { keys: ['y', 'Enter'], descriptionKey: 'shortcuts.outliers.unassign' },
      { keys: ['n'], descriptionKey: 'shortcuts.outliers.confirm' },
      { keys: ['x'], descriptionKey: 'shortcuts.outliers.select' },
      { keys: ['Ctrl+A'], descriptionKey: 'shortcuts.outliers.selectAll' },
      { keys: ['Esc'], descriptionKey: 'shortcuts.outliers.escape' },
    ],
  },
  {
    titleKey: 'shortcuts.groups.review',
    entries: [
      { keys: ['→', 'y'], descriptionKey: 'shortcuts.review.yes' },
      { keys: ['←', 'n'], descriptionKey: 'shortcuts.review.no' },
      { keys: ['Space', '↓'], descriptionKey: 'shortcuts.review.skip' },
      { keys: ['z', 'Ctrl+Z'], descriptionKey: 'shortcuts.review.undo' },
      { keys: ['Esc'], descriptionKey: 'shortcuts.review.leave' },
    ],
  },
]
