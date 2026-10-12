import { Outlet } from 'react-router-dom'

import { Layout } from '../components/Layout'
import { NotFoundPage } from '../pages/NotFoundPage'

import { useProject } from './ProjectContext'

/**
 * The element of `/p/:slug`: the project's pages when the account belongs to
 * the project the address names, and otherwise the very same not-found page an
 * unknown address gets, inside the same shell — a project the reader is not a
 * member of must look exactly like one that does not exist.
 */
export function ProjectGate() {
  const { status } = useProject()
  if (status === 'not-member') {
    return (
      <Layout>
        <NotFoundPage />
      </Layout>
    )
  }
  return <Outlet />
}
