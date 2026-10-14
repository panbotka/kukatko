import { ApiError } from '../../services/auth'

/** The form fields an API validation error can be attributed to. */
export type FormField = 'username' | 'password' | 'email' | 'instanceRole' | 'note'

/** The i18n keys for the validation messages the backend can produce. */
export type ErrorKey =
  | 'systemUsers.errors.usernameTaken'
  | 'systemUsers.errors.usernameRequired'
  | 'systemUsers.errors.usernameTooLong'
  | 'systemUsers.errors.passwordTooShort'
  | 'systemUsers.errors.invalidEmail'
  | 'systemUsers.errors.invalidRole'
  | 'systemUsers.errors.noteTooLong'
  | 'systemUsers.errors.lastMaintainer'
  | 'systemUsers.errors.accountBlocked'
  | 'systemUsers.errors.notAllowed'
  | 'systemUsers.errors.gone'
  | 'systemUsers.errors.generic'

/**
 * A failed submission: `field` names the input to flag inline, or is null when
 * the failure belongs to no single field and has to surface as a form-level alert.
 */
export interface FormError {
  field: FormField | null
  messageKey: ErrorKey
}

/**
 * Attributes a failed create/update/rename/password change to the field the
 * backend rejected.
 *
 * The account handlers (`internal/auth/handlers_admin.go`) answer with a plain
 * `{"error": "..."}` envelope rather than a per-field structure, so the status
 * plus a keyword from the message is all there is to go on: 409 is either a
 * duplicate username or the last-maintainer guard (told apart by the message),
 * and the possible 400s each name their own field. Anything unrecognised
 * degrades to a form-level message.
 *
 * The last-maintainer refusal belongs to no input — the block button has no form
 * at all — so it surfaces as a form-level alert.
 */
export function fieldErrorFor(error: unknown): FormError {
  if (error instanceof ApiError) {
    if (error.status === 409) {
      if (error.message.toLowerCase().includes('maintainer')) {
        return { field: null, messageKey: 'systemUsers.errors.lastMaintainer' }
      }
      return { field: 'username', messageKey: 'systemUsers.errors.usernameTaken' }
    }
    if (error.status === 400) {
      return badRequestError(error.message.toLowerCase())
    }
    if (error.status === 403) {
      return { field: null, messageKey: 'systemUsers.errors.notAllowed' }
    }
    if (error.status === 404) {
      return { field: null, messageKey: 'systemUsers.errors.gone' }
    }
  }
  return { field: null, messageKey: 'systemUsers.errors.generic' }
}

/**
 * Attributes a 400 by the keyword its message carries. The username is tried
 * first: a username message would otherwise be taken for none of the others,
 * and a rename sends nothing but the name.
 */
function badRequestError(message: string): FormError {
  if (message.includes('username')) {
    return {
      field: 'username',
      messageKey: message.includes('required')
        ? 'systemUsers.errors.usernameRequired'
        : 'systemUsers.errors.usernameTooLong',
    }
  }
  if (message.includes('password')) {
    return { field: 'password', messageKey: 'systemUsers.errors.passwordTooShort' }
  }
  if (message.includes('email')) {
    return { field: 'email', messageKey: 'systemUsers.errors.invalidEmail' }
  }
  if (message.includes('role')) {
    return { field: 'instanceRole', messageKey: 'systemUsers.errors.invalidRole' }
  }
  if (message.includes('note')) {
    return { field: 'note', messageKey: 'systemUsers.errors.noteTooLong' }
  }
  return { field: null, messageKey: 'systemUsers.errors.generic' }
}

/**
 * Explains a failed **row action that carries no form** — blocking or
 * unblocking an account, issuing a reset link. Those have no input to flag, and
 * their statuses mean something else than they do on a create or an update,
 * which is why {@link fieldErrorFor} must not be used for them: a 409 here is
 * either the last-maintainer guard (blocking) or a blocked account (a reset link
 * it could never use), never a taken username.
 *
 * Each answer says what to do about it — unblock the account first, promote
 * somebody else, reload the list — rather than "the action could not be
 * completed", which leaves the reader with nowhere to go.
 */
export function actionErrorFor(error: unknown): ErrorKey {
  if (error instanceof ApiError) {
    switch (error.status) {
      case 403:
        return 'systemUsers.errors.notAllowed'
      case 404:
        return 'systemUsers.errors.gone'
      case 409:
        return error.message.toLowerCase().includes('maintainer')
          ? 'systemUsers.errors.lastMaintainer'
          : 'systemUsers.errors.accountBlocked'
      default:
        break
    }
  }
  return 'systemUsers.errors.generic'
}
