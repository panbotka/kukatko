/**
 * The project guard's answer when the project a request names may not be
 * entered (any more): `404 {"error":"project not found"}`. Inside an open
 * project it means something changed under the page — the project was
 * deactivated, or the account removed from it — and the session the app holds
 * is out of date.
 */
export const PROJECT_NOT_FOUND_MESSAGE = 'project not found'

/** Who is told when a request runs into {@link PROJECT_NOT_FOUND_MESSAGE}. */
const listeners = new Set<() => void>()

/**
 * Subscribes `listener` to "a request found its project gone"; returns the
 * unsubscribe. `AuthProvider` re-reads the session on it, so the project gate
 * shows what became of the project instead of a page of failed loads.
 */
export function onProjectGone(listener: () => void): () => void {
  listeners.add(listener)
  return () => {
    listeners.delete(listener)
  }
}

/**
 * Looks at one failed API response and tells the {@link onProjectGone}
 * listeners when it is the project guard's refusal. Every service turns a
 * failed response into an `ApiError`, whose constructor calls this, so no
 * service has to know.
 */
export function reportApiError(status: number, message: string): void {
  if (status !== 404 || message !== PROJECT_NOT_FOUND_MESSAGE) {
    return
  }
  for (const listener of listeners) {
    listener()
  }
}
