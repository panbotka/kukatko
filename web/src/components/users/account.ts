import { type AdminUser } from '../../services/users'

/**
 * The two component-free questions the user roster asks about one account. They
 * live apart from the badges that render their answers because the page asks
 * them too — to count the waiting accounts and to filter the list — and because
 * a module that exports both a component and a plain function loses Fast Refresh.
 */

/**
 * The reserved top-level domain (RFC 6761) every placeholder address sits in,
 * lower-cased. It mirrors `mailer.invalidTLD` on the backend, which refuses to
 * dial such a recipient at all, and `auth.placeholderDomain`, which mints them.
 */
const PLACEHOLDER_TLD = 'invalid'

/**
 * Reports whether `email` is a placeholder rather than a mailbox anybody can be
 * reached at.
 *
 * Accounts that predate the mandatory-address rule — and the bootstrap
 * maintainer, created before anybody could have configured one — carry a
 * generated address in the reserved `.invalid` domain (migration 0063). It
 * parses like an address and is stored like one, which is exactly why it must
 * never be *shown* like one: nothing sent to it can arrive, so an administrator
 * who trusts it is waiting for a mail that will never be delivered.
 *
 * The test is on the last domain label, the same one the backend refuses on, so
 * it holds for any future placeholder domain under `.invalid`.
 */
export function isPlaceholderEmail(email: string): boolean {
  const at = email.lastIndexOf('@')
  if (at < 0) {
    return false
  }
  const labels = email.slice(at + 1).split('.')
  return labels[labels.length - 1].toLowerCase() === PLACEHOLDER_TLD
}

/**
 * Reports whether nobody has let this account in yet — `approved_at` is
 * explicitly null, which is what self-service registration leaves behind
 * (`auth.ErrNotApproved`: the credentials work, the sign-in still does not).
 *
 * An **absent** key is deliberately not treated as waiting. The backend always
 * sends `approved_at` (`json:"approved_at"`, no `omitempty`), so undefined means
 * "this payload does not say" — badging every such account as waiting would put
 * a call to action on rows that need none.
 */
export function isWaitingForApproval(user: Pick<AdminUser, 'approved_at'>): boolean {
  return user.approved_at === null
}
