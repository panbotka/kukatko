import { useEffect, useState } from 'react'

import { fetchFooterText } from '../services/settings'

/** Somebody who wants to hear that a project's footer line was just saved. */
type FooterListener = (slug: string, text: string) => void

/** Every mounted {@link useProjectFooter}, told about a save without a refetch. */
const listeners = new Set<FooterListener>()

/**
 * Tells every footer on the screen that the project `slug` now ends its pages
 * with `text`. The settings page calls it after the server confirmed a save, so
 * the footer below that very form changes with it instead of after a reload.
 */
export function announceFooterChanged(slug: string, text: string): void {
  for (const listener of listeners) {
    listener(slug, text)
  }
}

/** The line that was read, and for which project. */
interface Loaded {
  slug: string
  text: string
}

/**
 * The footer line of the project `slug` — the plain text its admin set in the
 * project's settings (`GET /p/<slug>/settings/footer`) — or `''` for none.
 * `null` asks for nothing and answers `''`: an instance page, or a project the
 * account may not enter, shows no project's line.
 *
 * It answers only for the project it was asked about: while another project's
 * line is still on its way after a switch, it says nothing rather than show the
 * previous project's sentence under the new project's pages. A failed read is
 * no line either — the footer is no place for an error.
 */
export function useProjectFooter(slug: string | null): string {
  const [loaded, setLoaded] = useState<Loaded | null>(null)

  useEffect(() => {
    if (slug === null) {
      return undefined
    }
    const controller = new AbortController()
    fetchFooterText(slug, controller.signal)
      .then((text) => {
        setLoaded({ slug, text })
      })
      .catch((error: unknown) => {
        if (error instanceof DOMException && error.name === 'AbortError') {
          return
        }
        setLoaded({ slug, text: '' })
      })
    return () => {
      controller.abort()
    }
  }, [slug])

  useEffect(() => {
    if (slug === null) {
      return undefined
    }
    // Another project's save is none of this footer's business.
    const listener: FooterListener = (changed, text) => {
      if (changed === slug) {
        setLoaded({ slug, text })
      }
    }
    listeners.add(listener)
    return () => {
      listeners.delete(listener)
    }
  }, [slug])

  return slug !== null && loaded?.slug === slug ? loaded.text : ''
}
