import { useCallback, useState } from 'react'

import {
  readSettings,
  sanitizeSettings,
  type SlideshowEffect,
  type SlideshowSettings,
  writeSettings,
} from '../lib/slideshowSettings'

/** Result of {@link useSlideshowSettings}: the current settings plus setters. */
export interface UseSlideshowSettingsResult {
  /** The current (persisted, sanitised) preferences. */
  settings: SlideshowSettings
  /** Sets the transition effect and persists it. */
  setEffect: (effect: SlideshowEffect) => void
  /** Sets the auto-advance interval (ms) and persists it. */
  setIntervalMs: (intervalMs: number) => void
}

/**
 * Reads the persisted slideshow preferences once on mount and exposes setters
 * that update state and write back to localStorage, so the user's chosen effect
 * and speed survive reloads and other slideshows. Values are sanitised on every
 * write, so an out-of-range update can never corrupt the stored settings.
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

  const setEffect = useCallback(
    (effect: SlideshowEffect) => {
      update({ effect })
    },
    [update],
  )
  const setIntervalMs = useCallback(
    (intervalMs: number) => {
      update({ intervalMs })
    },
    [update],
  )

  return { settings, setEffect, setIntervalMs }
}
