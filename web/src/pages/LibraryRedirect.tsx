import { Navigate, useLocation } from 'react-router-dom'

import { LIBRARY_PATH } from '../lib/libraryView'

/**
 * Compatibility shim for `/library` **and everything under it**: the library now
 * lives at {@link LIBRARY_PATH} (`/`), so this forwards the whole retired branch
 * to the homepage.
 *
 * Two generations of dead links land here. Kukátko's own bookmarks, shared links
 * and saved searches minted before the library became the homepage carry a bare
 * `/library`. Deeper ones are inherited: this instance took over the domain that
 * used to serve PhotoPrism, which kept its entire UI under `/library/…`, so
 * browser history and address-bar autocomplete keep aiming at addresses like
 * `/library/login`. Those used to fall through to the 404 page — and, worst of
 * all, *after* a successful sign-in, because the route guard stashes the address
 * you asked for and login faithfully returns you to it.
 *
 * The search and hash are passed through verbatim — including keys the library
 * view does not know — so an old link's filters, sort and any extra params
 * survive the hop. The redirect *replaces* the history entry: without that,
 * pressing Back from the library would land on `/library`, be redirected again,
 * and trap the user on the page they tried to leave.
 */
export function LibraryRedirect() {
  const { search, hash } = useLocation()

  return <Navigate to={{ pathname: LIBRARY_PATH, search, hash }} replace />
}
