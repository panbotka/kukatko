import { useCallback, useState } from 'react'

import {
  readSettings,
  sanitizeSettings,
  type SlideshowSettings,
  writeSettings,
} from '../lib/slideshowSettings'

/** Result of {@link useSlideshowSettings}: the current settings plus one setter. */
export interface UseSlideshowSettingsResult {
  /** The current (persisted, sanitised) preferences. */
  settings: SlideshowSettings
  /**
   * Applies a patch to the settings and persists the result. One setter rather
   * than one per field, because the settings form edits them all and every
   * change takes the same route: sanitise, store, apply from the current slide on.
   */
  update: (patch: Partial<SlideshowSettings>) => void
}

/**
 * Reads the persisted slideshow preferences once on mount and exposes a setter
 * that updates state and writes back to localStorage, so the user's chosen
 * effect, speed, repeat/shuffle and captions survive reloads and other
 * slideshows. Values are sanitised on every write, so an out-of-range update can
 * never corrupt the stored settings.
 *
 * The start dialog deliberately does *not* use this hook: it edits a draft and
 * persists it only when the reader confirms, so dismissing the dialog changes
 * nothing.
 */
export function useSlideshowSettings(): UseSlideshowSettingsResult {
  const [settings, setSettings] = useState<SlideshowSettings>(() => readSettings())

  const update = useCallback((patch: Partial<SlideshowSettings>) => {
    setSettings((prev) => {
      const next = sanitizeSettings({ ...prev, ...patch })
      writeSettings(next)
      return next
    })
  }, [])

  return { settings, update }
}
