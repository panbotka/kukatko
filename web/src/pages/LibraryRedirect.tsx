import { Navigate, useLocation } from 'react-router-dom'

import { LIBRARY_PATH } from '../lib/libraryView'

/**
 * Compatibility shim for the retired `/library` route: the library now lives at
 * {@link LIBRARY_PATH} (`/`). Bookmarks, shared links and saved searches minted
 * before the swap still carry `/library`, so this forwards them to the homepage.
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
