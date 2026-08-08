import Spinner from 'react-bootstrap/Spinner'
import { useTranslation } from 'react-i18next'
import { Navigate, Outlet, useLocation } from 'react-router-dom'

import { ForbiddenPage } from '../pages/ForbiddenPage'
import { type GuardRole, roleAtLeast } from '../services/auth'

import { useAuth } from './AuthContext'

/** Centered full-height spinner shown while the session is still loading. */
function FullPageSpinner() {
  const { t } = useTranslation()
  return (
    <div className="d-flex justify-content-center align-items-center" style={{ minHeight: '60vh' }}>
      <Spinner animation="border" role="status">
        <span className="visually-hidden">{t('auth.loading')}</span>
      </Spinner>
    </div>
  )
}

/**
 * Guards nested routes: while the session loads it shows a spinner; once
 * resolved, unauthenticated visitors are redirected to `/login` with the
 * originally requested location stashed in history state so login can return
 * them there.
 */
export function RequireAuth() {
  const { status } = useAuth()
  const location = useLocation()

  if (status === 'loading') {
    return <FullPageSpinner />
  }
  if (status !== 'authenticated') {
    return <Navigate to="/login" replace state={{ from: location }} />
  }
  return <Outlet />
}

/**
 * Guards nested routes by minimum role. Assumes an authenticated user (nest it
 * inside {@link RequireAuth}); users below `role` get the {@link ForbiddenPage}
 * **in place of** the route.
 *
 * It deliberately does not redirect. Bouncing to the library dropped the user on
 * a page they had not asked for with no explanation — indistinguishable from a
 * broken link — and threw away the address, so a reload could not even show what
 * went wrong. Rendering in place keeps the URL, and the sentence, where the user
 * put them.
 */
export function RequireRole({ role }: { role: GuardRole }) {
  const { role: current } = useAuth()

  if (current === null) {
    return <Navigate to="/login" replace />
  }
  if (!roleAtLeast(current, role)) {
    return <ForbiddenPage role={role} />
  }
  return <Outlet />
}

/**
 * Guards nested routes behind import permission. Import is an operations
 * capability, so it requires a maintainer (the top of the ladder). Named after
 * the capability it gates rather than the role, mirroring the backend's
 * `RequireImport` middleware; the equivalent {@link RequireRole} threshold is
 * `role="maintainer"`. Users without it get the {@link ForbiddenPage} in place of
 * the route, for the same reason {@link RequireRole} does.
 */
export function RequireImport() {
  const { canImport } = useAuth()

  if (!canImport) {
    return <ForbiddenPage role="maintainer" />
  }
  return <Outlet />
}
