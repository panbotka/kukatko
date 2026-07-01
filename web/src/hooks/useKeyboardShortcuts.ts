import { useEffect, useRef } from 'react'

import { isTypingElement } from '../lib/ratingHotkeys'
import { isFormModalOpen, shortcutToken } from '../lib/shortcuts'

/** A handler invoked when its shortcut key fires; receives the raw event. */
export type ShortcutHandler = (event: KeyboardEvent) => void

/**
 * Maps a normalized {@link shortcutToken} to the handler that runs for it. Only a
 * subset of tokens is ever bound, so lookups are optional by construction.
 */
export type ShortcutMap = Partial<Record<string, ShortcutHandler>>

/** Options for {@link useKeyboardShortcuts}. */
export interface UseKeyboardShortcutsOptions {
  /** When false the listener is inert (still bound, but ignores every key). */
  enabled?: boolean
}

/**
 * Binds a single document-level `keydown` listener that dispatches to `handlers`
 * keyed by {@link shortcutToken}. It is the shared plumbing behind every keyboard
 * shortcut in the app: the grid navigation, the detail page and the help overlay.
 *
 * Shortcuts never fire while a modifier (Ctrl/Meta/Alt) is held, while the user
 * is typing in an input/textarea/`contenteditable`, or while a modal containing a
 * form is open — so text entry and dialogs are never hijacked. A matched key has
 * its default prevented and the handler run. `handlers`/`enabled` are read through
 * refs so the listener is bound once yet always sees the latest closures.
 */
export function useKeyboardShortcuts(
  handlers: ShortcutMap,
  options: UseKeyboardShortcutsOptions = {},
): void {
  const { enabled = true } = options
  const handlersRef = useRef(handlers)
  handlersRef.current = handlers
  const enabledRef = useRef(enabled)
  enabledRef.current = enabled

  useEffect(() => {
    function onKeyDown(event: KeyboardEvent) {
      if (!enabledRef.current) {
        return
      }
      // Leave OS/browser chords (Ctrl/Cmd/Alt) alone; Shift is allowed so `?`
      // (Shift+/) can open the help overlay.
      if (event.ctrlKey || event.metaKey || event.altKey) {
        return
      }
      if (isTypingElement(event.target) || isFormModalOpen()) {
        return
      }
      const handler = handlersRef.current[shortcutToken(event.key)]
      if (handler === undefined) {
        return
      }
      event.preventDefault()
      handler(event)
    }
    document.addEventListener('keydown', onKeyDown)
    return () => {
      document.removeEventListener('keydown', onKeyDown)
    }
  }, [])
}
