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
  // Old engines (and a few remotes) still report the space bar as `Spacebar`;
  // normalizing it here means a handler only ever has to bind `' '`.
  if (key === 'Spacebar') {
    return ' '
  }
  return key.length === 1 ? key.toLowerCase() : key
}

/**
 * Reports whether an event target activates on Space (or Enter) by itself — a
 * button, a link, an `role="button"` or a `<summary>`. A global Space/Enter
 * shortcut has to stand aside for those: with the "Select all" button focused,
 * one press would otherwise both click it and run the shortcut, and the reader
 * would see two things happen for one key. Text-entry elements are covered by
 * `isTypingElement` instead.
 */
export function isActivatableElement(target: EventTarget | null): boolean {
  if (!(target instanceof HTMLElement)) {
    return false
  }
  const tag = target.tagName
  return (
    tag === 'BUTTON' ||
    tag === 'A' ||
    tag === 'SUMMARY' ||
    target.getAttribute('role') === 'button' ||
    target.getAttribute('role') === 'link'
  )
}

/**
 * The keys that a focused control owns before any global shortcut does: Space
 * (and Enter) press the button/link under the keyboard focus. Dispatch skips
 * these when {@link isActivatableElement} says so — see
 * `useKeyboardShortcuts`.
 */
export const ACTIVATION_KEYS: readonly string[] = [' ', 'Enter']

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
    titleKey: 'shortcuts.groups.global',
    entries: [
      { keys: ['/', 'Ctrl+K'], descriptionKey: 'shortcuts.global.search' },
      { keys: ['?'], descriptionKey: 'shortcuts.global.help' },
    ],
  },
  {
    titleKey: 'shortcuts.groups.grid',
    entries: [
      { keys: ['↑', '↓', '←', '→', 'j', 'k', 'h', 'l'], descriptionKey: 'shortcuts.grid.move' },
      { keys: ['Enter'], descriptionKey: 'shortcuts.grid.open' },
      { keys: ['f'], descriptionKey: 'shortcuts.grid.favorite' },
    ],
  },
  {
    titleKey: 'shortcuts.groups.selection',
    entries: [
      { keys: ['Space', 'x'], descriptionKey: 'shortcuts.selection.toggle' },
      { keys: ['Shift'], descriptionKey: 'shortcuts.selection.range' },
      { keys: ['Esc'], descriptionKey: 'shortcuts.selection.escape' },
    ],
  },
  {
    titleKey: 'shortcuts.groups.detail',
    entries: [
      { keys: ['←', '→'], descriptionKey: 'shortcuts.detail.prevNext' },
      { keys: ['f'], descriptionKey: 'shortcuts.detail.favorite' },
      { keys: ['m'], descriptionKey: 'shortcuts.detail.faces' },
      { keys: ['i'], descriptionKey: 'shortcuts.detail.info' },
      // `s` as in „skrýt"; `h` is taken by the grid's vim-style move-left.
      { keys: ['s'], descriptionKey: 'shortcuts.detail.hide' },
      { keys: ['0', '…', '5'], descriptionKey: 'shortcuts.detail.rating' },
      { keys: ['p', 'r', 'v'], descriptionKey: 'shortcuts.detail.flag' },
      { keys: ['Esc'], descriptionKey: 'shortcuts.detail.back' },
    ],
  },
  {
    titleKey: 'shortcuts.groups.video',
    entries: [
      { keys: ['k'], descriptionKey: 'shortcuts.video.playPause' },
      { keys: ['j', 'l'], descriptionKey: 'shortcuts.video.skip' },
      { keys: ['<', '>'], descriptionKey: 'shortcuts.video.speed' },
      { keys: ['←', '→'], descriptionKey: 'shortcuts.video.arrows' },
    ],
  },
  {
    titleKey: 'shortcuts.groups.slideshow',
    entries: [
      { keys: ['←', '→', 'PgUp', 'PgDn'], descriptionKey: 'shortcuts.slideshow.step' },
      { keys: ['Space'], descriptionKey: 'shortcuts.slideshow.playPause' },
      { keys: ['f'], descriptionKey: 'shortcuts.slideshow.fullscreen' },
      { keys: ['Tab'], descriptionKey: 'shortcuts.slideshow.chrome' },
      { keys: ['Esc'], descriptionKey: 'shortcuts.slideshow.leave' },
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
    titleKey: 'shortcuts.groups.lightbox',
    entries: [
      { keys: ['←', '→'], descriptionKey: 'shortcuts.lightbox.step' },
      { keys: ['o'], descriptionKey: 'shortcuts.lightbox.open' },
      { keys: ['Esc'], descriptionKey: 'shortcuts.lightbox.close' },
    ],
  },
  {
    titleKey: 'shortcuts.groups.compare',
    entries: [
      { keys: ['←'], descriptionKey: 'shortcuts.compare.keepLeft' },
      { keys: ['→'], descriptionKey: 'shortcuts.compare.keepRight' },
      { keys: ['b'], descriptionKey: 'shortcuts.compare.keepBoth' },
      { keys: ['Esc'], descriptionKey: 'shortcuts.compare.leave' },
    ],
  },
  {
    titleKey: 'shortcuts.groups.review',
    entries: [
      { keys: ['→', 'y'], descriptionKey: 'shortcuts.review.yes' },
      { keys: ['←', 'n'], descriptionKey: 'shortcuts.review.no' },
      { keys: ['Space', '↓'], descriptionKey: 'shortcuts.review.skip' },
      { keys: ['↵'], descriptionKey: 'shortcuts.review.next' },
      { keys: ['z', 'Ctrl+Z'], descriptionKey: 'shortcuts.review.undo' },
      { keys: ['o'], descriptionKey: 'shortcuts.review.open' },
      { keys: ['Esc'], descriptionKey: 'shortcuts.review.leave' },
    ],
  },
]
