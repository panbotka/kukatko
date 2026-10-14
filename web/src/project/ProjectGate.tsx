import { Outlet, useParams } from 'react-router-dom'

import { Layout } from '../components/Layout'
import { NotFoundPage } from '../pages/NotFoundPage'

import { useProject } from './ProjectContext'

/**
 * The element of `/p/:slug`: the project's pages when the account belongs to
 * the project the address names, and otherwise the very same not-found page an
 * unknown address gets, inside the same shell — a project the reader is not a
 * member of must look exactly like one that does not exist.
 *
 * The outlet is keyed by the project, so **a project switch remounts every page
 * below it**. Without the key, `/p/a/albums` → `/p/b/albums` matches the same
 * route element at the same place, React keeps the page, and its fetch effects
 * — keyed on the page's own inputs, never on the project — leave project a's
 * albums on screen under b's name, with every click sending a's uids to b's
 * API. Remounting throws away all of a page's state at once (its data, its
 * selection, its upload queue) and runs the effects' cleanups, which abort the
 * requests still in flight for a, so a late answer for a has nowhere to land.
 * Page code therefore never has to put the project in its dependencies.
 */
export function ProjectGate() {
  const { status } = useProject()
  const { slug } = useParams()
  if (status === 'not-member') {
    return (
      <Layout>
        <NotFoundPage />
      </Layout>
    )
  }
  return <Outlet key={slug} />
}
