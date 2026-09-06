import { usePublicSettings, type PublicSettingsState } from './usePublicSettings'

/**
 * Whether this instance sends transactional mail, as read off the public
 * settings state.
 *
 * An instance that could not be asked counts as "no mail": the flag only ever
 * decides whether a sentence may promise an e-mail, and a promise made on a
 * guess is the very thing this flag exists to stop. The wording without the
 * promise is true either way — somebody is approving the account regardless of
 * how they say so.
 *
 * @param state the public settings as {@link usePublicSettings} reports them.
 */
export function mailEnabledFrom(state: PublicSettingsState): boolean {
  return state.status === 'ready' && state.settings.mail_enabled
}

/**
 * Asks `GET /api/v1/settings/public` whether this instance sends mail at all.
 *
 * It is a reading of {@link usePublicSettings}, so a screen that also needs
 * another public fact (the sign-in card, the registration form) reads that hook
 * once and calls {@link mailEnabledFrom} instead of mounting this one beside it —
 * each hook fetches on its own mount, and two hooks would ask the server twice.
 */
export function useMailEnabled(): boolean {
  return mailEnabledFrom(usePublicSettings())
}
