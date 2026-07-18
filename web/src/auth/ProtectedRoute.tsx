import Spinner from 'react-bootstrap/Spinner'
import { useTranslation } from 'react-i18next'
import { Navigate, Outlet, useLocation } from 'react-router-dom'

import { roleAtLeast, type Role } from '../services/auth'

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
 * inside {@link RequireAuth}); users below `role` are sent to the home page.
 */
export function RequireRole({ role }: { role: Role }) {
  const { role: current } = useAuth()

  if (current === null) {
    return <Navigate to="/login" replace />
  }
  if (!roleAtLeast(current, role)) {
    return <Navigate to="/" replace />
  }
  return <Outlet />
}

/**
 * Guards nested routes behind import permission. Import is an operations
 * capability, so it requires a maintainer (the top of the ladder). Named after
 * the capability it gates rather than the role, mirroring the backend's
 * `RequireImport` middleware; the equivalent {@link RequireRole} threshold is
 * `role="maintainer"`. Users without it are sent to the home page.
 */
export function RequireImport() {
  const { canImport } = useAuth()

  if (!canImport) {
    return <Navigate to="/" replace />
  }
  return <Outlet />
}
