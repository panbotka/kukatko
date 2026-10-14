import type { ProjectPublicState } from './useProjectPublicSettings'

/**
 * What a project says about self-service registration.
 *
 * `unknown` is not a synonym for `closed`: it means the question could not be
 * asked at all (an unreachable backend, a 5xx). The callers answer it
 * differently on purpose — the sign-in screen hides the invitation, because
 * advertising a door that may not open is worse than not mentioning it, while
 * the registration screen still shows its form and lets the server have the
 * final word. `notFound` is a slug that names no project the server will admit
 * (unknown, deactivated, or with registration closed — one 404); there is no
 * form to show for it.
 */
export type RegistrationState = 'loading' | 'open' | 'closed' | 'unknown' | 'notFound'

/**
 * Narrows a project's already-fetched public settings (see
 * `useProjectPublicSettings`) to the registration question. No project at all
 * (`none`) is `closed`: registration always goes through a project.
 *
 * @param state the project's public settings as the hook reports them.
 */
export function registrationOpenFrom(state: ProjectPublicState): RegistrationState {
  switch (state.status) {
    case 'loading':
    case 'unknown':
    case 'notFound':
      return state.status
    case 'none':
      return 'closed'
    case 'ready':
      return state.settings.registration_enabled ? 'open' : 'closed'
  }
}
