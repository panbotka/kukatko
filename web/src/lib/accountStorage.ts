/**
 * The storage key an account's own copy of `prefix` lives under: the prefix
 * completed by the account's uid, the pattern `lib/pushPromptAnswer.ts` set.
 * Two people share one browser, so anything recording what an *account* has
 * seen, dismissed, played or scrolled carries its owner in the key — and a key
 * with somebody else's uid in it is simply never read, which is what makes the
 * other account's state read as absent rather than be honoured.
 *
 * Returns null without a user: no account, no key. Callers treat that as
 * "nothing stored" when reading and as a no-op when writing, which is the safe
 * direction everywhere this is used (the banner shows, the panel shows, the
 * grid starts at the top).
 */
export function accountStorageKey(prefix: string, user: string | undefined): string | null {
  return user === undefined || user === '' ? null : `${prefix}.${user}`
}

/**
 * The storage key an account's copy of `prefix` **in one project** lives under:
 * `<prefix>.<uid>.<slug>`. For what an account saw, dismissed or played in a
 * library — a digest, a daily round — which is about that library's photos and
 * must neither carry over into another project nor be overwritten from there.
 * The account comes first, so {@link isAccountStorageKey} and the sign-out sweep
 * claim it like any other account key.
 *
 * Returns null without a user or without a project, read the same way as
 * {@link accountStorageKey}'s null: nothing stored, nothing written.
 */
export function accountProjectStorageKey(
  prefix: string,
  user: string | undefined,
  project: string | null | undefined,
): string | null {
  const key = accountStorageKey(prefix, user)
  return key === null || project === undefined || project === null || project === ''
    ? null
    : `${key}.${project}`
}

/**
 * Whether `key` is `prefix` itself or one account's copy of it
 * (`prefix.<uid>`). The dot is part of the match, so `kukatko.review.daily`
 * never claims `kukatko.review.dailyish`.
 */
export function isAccountStorageKey(key: string, prefix: string): boolean {
  return key === prefix || key.startsWith(`${prefix}.`)
}
