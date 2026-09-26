/**
 * localStorage key prefix under which an account's answer to the notification
 * prompt is remembered; the account's uid completes it. Per account because
 * two people may share one browser, per browser because a subscription is a
 * property of the browser — a new one is a new question.
 */
const STORAGE_PREFIX = 'kukatko.pushPrompt.answered.'

/**
 * Reports whether the account `userUid` has already answered the notification
 * prompt in this browser — turned them on, said "not now", closed the dialog or
 * had the browser block it. Storage that cannot be read (private mode, disabled)
 * counts as answered: a prompt that could never remember its answer would come
 * back on every page load, which is exactly the nagging it exists to avoid.
 */
export function hasAnsweredPushPrompt(userUid: string): boolean {
  try {
    return window.localStorage.getItem(STORAGE_PREFIX + userUid) !== null
  } catch {
    return true
  }
}

/**
 * Remembers that `userUid` has answered the notification prompt in this
 * browser, stamped with when. Failures (storage disabled / quota) are
 * swallowed: the prompt closes regardless.
 */
export function recordPushPromptAnswer(userUid: string, now: Date = new Date()): void {
  try {
    window.localStorage.setItem(STORAGE_PREFIX + userUid, now.toISOString())
  } catch {
    // Best-effort: ignore storage failures.
  }
}
