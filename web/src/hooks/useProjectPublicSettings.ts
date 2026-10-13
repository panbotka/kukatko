import { useEffect, useState } from 'react'

import { ApiError } from '../services/auth'
import { fetchProjectPublicSettings, type ProjectPublicSettings } from '../services/settings'

/**
 * What an anonymous screen managed to learn about one project.
 *
 * `none` means there was no project to ask about. `notFound` is the server's
 * 404 — a slug nobody has, or a deactivated project, which it does not tell
 * apart. `unknown` is not a synonym for either: the question could not be asked
 * at all (an unreachable backend, a 5xx), and each caller decides what that
 * silence means for the affordance it is about to draw.
 */
export type ProjectPublicState =
  | { status: 'none' }
  | { status: 'loading' }
  | { status: 'ready'; settings: ProjectPublicSettings }
  | { status: 'notFound' }
  | { status: 'unknown' }

/**
 * Asks `GET /api/v1/p/<slug>/settings/public` once what anybody may know about
 * the project `slug`: its name and whether it takes registrations.
 *
 * The endpoint is anonymous, so this works on the project's registration page
 * and on the sign-in screen alike. A `null` slug asks nothing and reports
 * `none`. It fetches once per slug and never polls; the request is aborted when
 * the slug changes or the component unmounts, and an aborted request leaves the
 * state alone.
 *
 * @param slug the project's URL slug, or `null` when there is no project.
 */
export function useProjectPublicSettings(slug: string | null): ProjectPublicState {
  // The answer is kept with the slug it belongs to, so a changed slug reads as
  // "loading" until its own answer arrives — without resetting state in the effect.
  const [answer, setAnswer] = useState<{ slug: string; state: ProjectPublicState } | null>(null)

  useEffect(() => {
    if (slug === null) {
      return
    }
    const controller = new AbortController()
    fetchProjectPublicSettings(slug, controller.signal)
      .then((settings) => {
        if (!controller.signal.aborted) {
          setAnswer({ slug, state: { status: 'ready', settings } })
        }
      })
      .catch((error: unknown) => {
        if (controller.signal.aborted) {
          return
        }
        const state: ProjectPublicState =
          error instanceof ApiError && error.status === 404
            ? { status: 'notFound' }
            : { status: 'unknown' }
        setAnswer({ slug, state })
      })
    return () => {
      controller.abort()
    }
  }, [slug])

  if (slug === null) {
    return { status: 'none' }
  }
  return answer?.slug === slug ? answer.state : { status: 'loading' }
}
