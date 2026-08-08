import { useEffect } from 'react'
import { useTranslation } from 'react-i18next'

/**
 * Names the browser tab after the page it shows: `document.title` becomes
 * `<page> · Kukátko` (`documentTitle.page`) while the calling component is
 * mounted, and falls back to the bare app name (`documentTitle.app`) when there
 * is nothing to say yet.
 *
 * The tab title is not decoration — it is what the **browser history**, a second
 * tab and a bookmark are labelled with. A gallery is browsed by "find the photo I
 * saw last week", and a history list of fifty identical „Kukátko" entries answers
 * none of that; the same goes for two open tabs (opening a photo in a new tab is
 * ordinary here) and for a bookmarked saved view.
 *
 * **One hook, called by each page** rather than a router-side table: the
 * interesting titles are a photo's name, a person's, an album's — data the page
 * already holds and nothing else does. So the formatting, the reset and the
 * language handling live here once, and a page contributes only its own name.
 *
 * Pass `null`/`undefined` (or a blank string) while the name is still loading —
 * the tab then reads plain „Kukátko" instead of a stale or invented one.
 *
 * **Leaving resets it.** The effect's cleanup puts the app name back, so a page
 * that sets no title of its own can never inherit the previous one; React runs
 * the outgoing page's cleanup before the incoming page's effect, so a navigation
 * between two titled pages still lands on the new title, not the app name.
 *
 * The title goes through i18n like the rest of the UI — including the `·`
 * separator and the brand's position, which live in the `documentTitle.page`
 * pattern — and follows a language switch, because `t` changes identity with the
 * active language.
 */
export function useDocumentTitle(title: string | null | undefined): void {
  const { t } = useTranslation()
  // Normalize before the dependency array: a page that recomputes an equal
  // string on every render must not re-run the effect.
  const page = title?.trim() ?? ''

  useEffect(() => {
    const appName = t('documentTitle.app')
    document.title = page === '' ? appName : t('documentTitle.page', { title: page })
    return () => {
      document.title = appName
    }
  }, [page, t])
}
