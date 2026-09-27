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
 * Whether `key` is `prefix` itself or one account's copy of it
 * (`prefix.<uid>`). The dot is part of the match, so `kukatko.review.daily`
 * never claims `kukatko.review.dailyish`.
 */
export function isAccountStorageKey(key: string, prefix: string): boolean {
  return key === prefix || key.startsWith(`${prefix}.`)
}
