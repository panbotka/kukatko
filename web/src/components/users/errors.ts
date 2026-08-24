import { ApiError } from '../../services/auth'

/** The form fields an API validation error can be attributed to. */
export type FormField = 'username' | 'password' | 'email' | 'role' | 'note'

/** The i18n keys for the validation messages the backend can produce. */
export type ErrorKey =
  | 'users.errors.usernameTaken'
  | 'users.errors.passwordTooShort'
  | 'users.errors.invalidEmail'
  | 'users.errors.invalidRole'
  | 'users.errors.noteTooLong'
  | 'users.errors.lastMaintainer'
  | 'users.errors.accountBlocked'
  | 'users.errors.notAllowed'
  | 'users.errors.gone'
  | 'users.errors.generic'

/**
 * A failed submission: `field` names the input to flag inline, or is null when
 * the failure belongs to no single field and has to surface as a form-level alert.
 */
export interface FormError {
  field: FormField | null
  messageKey: ErrorKey
}

/**
 * Attributes a failed create/update to the field the backend rejected.
 *
 * The admin user handlers answer with a plain `{"error": "..."}` envelope rather
 * than a per-field structure, so the status plus a keyword from the message is
 * all there is to go on: 409 is either a duplicate username or the
 * last-maintainer guard (told apart by the message), and the four possible 400s
 * each name their own field (`internal/auth/handlers_admin.go`). Anything
 * unrecognised degrades to a form-level message.
 *
 * The last-maintainer refusal belongs to no input — the role select is only the
 * way it was triggered, and the disable button has no form at all — so it
 * surfaces as a form-level alert.
 */
export function fieldErrorFor(error: unknown): FormError {
  if (error instanceof ApiError) {
    if (error.status === 409) {
      if (error.message.toLowerCase().includes('maintainer')) {
        return { field: null, messageKey: 'users.errors.lastMaintainer' }
      }
      return { field: 'username', messageKey: 'users.errors.usernameTaken' }
    }
    if (error.status === 400) {
      const message = error.message.toLowerCase()
      if (message.includes('password')) {
        return { field: 'password', messageKey: 'users.errors.passwordTooShort' }
      }
      if (message.includes('email')) {
        return { field: 'email', messageKey: 'users.errors.invalidEmail' }
      }
      if (message.includes('role')) {
        return { field: 'role', messageKey: 'users.errors.invalidRole' }
      }
      if (message.includes('note')) {
        return { field: 'note', messageKey: 'users.errors.noteTooLong' }
      }
    }
  }
  return { field: null, messageKey: 'users.errors.generic' }
}

/**
 * Explains a failed **row action that carries no form** — approving an account,
 * issuing a reset link. Those have no input to flag, and their statuses mean
 * something else than they do on a create or an update, which is why
 * {@link fieldErrorFor} must not be used for them: a 409 here is never a taken
 * username but always a blocked account (`auth.ErrUserDisabled`), and a 403 is
 * the maintainer boundary rather than a missing role.
 *
 * Each answer says what to do about it — unblock the account first, ask a
 * maintainer, reload the roster — rather than "the action could not be
 * completed", which leaves the reader with nowhere to go.
 */
export function actionErrorFor(error: unknown): ErrorKey {
  if (error instanceof ApiError) {
    switch (error.status) {
      case 403:
        return 'users.errors.notAllowed'
      case 404:
        return 'users.errors.gone'
      case 409:
        return 'users.errors.accountBlocked'
      default:
        break
    }
  }
  return 'users.errors.generic'
}
