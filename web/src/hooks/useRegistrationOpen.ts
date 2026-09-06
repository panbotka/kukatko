import { usePublicSettings, type PublicSettingsState } from './usePublicSettings'

/**
 * What this instance says about self-service registration.
 *
 * `unknown` is not a synonym for `closed`: it means the question could not be
 * asked at all (an unreachable backend, a 5xx). The two callers answer it
 * differently on purpose — the sign-in screen hides the invitation, because
 * advertising a door that may not open is worse than not mentioning it, while
 * the registration screen still shows its form and lets the server have the
 * final word.
 */
export type RegistrationState = 'loading' | 'open' | 'closed' | 'unknown'

/**
 * Narrows an already-fetched public settings state to the registration question.
 *
 * A screen that needs a second public fact as well — the registration form,
 * which also decides whether it may promise an e-mail — reads
 * {@link usePublicSettings} once and calls this, rather than mounting two hooks
 * that would each fetch.
 *
 * @param state the public settings as {@link usePublicSettings} reports them.
 */
export function registrationOpenFrom(state: PublicSettingsState): RegistrationState {
  if (state.status === 'loading' || state.status === 'unknown') {
    return state.status
  }
  return state.settings.registration_enabled ? 'open' : 'closed'
}

/**
 * Asks `GET /api/v1/settings/public` once whether registration is open, and
 * narrows the answer to that one question.
 *
 * It is a reading of {@link usePublicSettings}, which fetches the whole public
 * record. A screen that needs both facts (the sign-in card, which also decides
 * whether to offer a passkey) reads that hook directly rather than pairing this
 * one with a sibling, so it asks the server once.
 */
export function useRegistrationOpen(): RegistrationState {
  return registrationOpenFrom(usePublicSettings())
}
