import { useTranslation } from 'react-i18next'
import { Link } from 'react-router-dom'

import { LIBRARY_PATH } from '../lib/libraryView'
import { type GuardRole } from '../services/auth'

/**
 * The i18n key of the explanation for each role a route guard can demand, kept
 * as an explicit map (rather than a template-literal key) so a typo is a compile
 * error, the typed `t` accepts it, and adding a role to the ladder fails the
 * build until it gets a sentence. Mirrors the pattern in {@link LeaderboardPage}.
 *
 * Each role gets its own sentence instead of one template with the role name
 * interpolated: Czech would have to decline it („roli editora", „roli správce
 * systému"), and the way to obtain the role differs — an editor asks an admin,
 * an admin asks someone who already is one.
 */
const MESSAGE_KEYS = {
  editor: 'forbidden.message.editor',
  admin: 'forbidden.message.admin',
  maintainer: 'forbidden.message.maintainer',
} as const

/**
 * Shown **in place of** a route the current role may not enter, instead of the
 * silent redirect to the library the guards used to do: a viewer who typed
 * `/review` — or followed a shared link to `/duplicates` — landed on a different
 * page than the one they asked for and never learned why, which reads as a
 * broken app rather than as a permission.
 *
 * Rendering rather than redirecting keeps the URL on the protected route, so a
 * reload shows the same explanation and Back returns to wherever the user came
 * from. Styled like {@link NotFoundPage} — its sibling in "this page is not for
 * you": one heading, one sentence, one way out.
 *
 * On the two fullscreen guarded routes (`/review`, `/duplicates/compare`) the
 * guard sits outside `Layout`, so this renders without the navbar; the link back
 * to the library is then the only way out, which is why it is always here.
 */
export function ForbiddenPage({ role }: { role: GuardRole }) {
  const { t } = useTranslation()

  return (
    <div className="text-center py-5" data-testid="forbidden-page">
      <h1 className="kk-page-title mb-3">{t('forbidden.title')}</h1>
      <p className="text-secondary mb-4">{t(MESSAGE_KEYS[role])}</p>
      <Link to={LIBRARY_PATH} className="btn btn-primary">
        {t('forbidden.back')}
      </Link>
    </div>
  )
}
