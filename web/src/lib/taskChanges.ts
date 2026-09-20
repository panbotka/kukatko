/**
 * A one-topic event bus: "something about the work queue changed from this
 * browser". The tasks and comments services fire it after every successful
 * write (a comment, a state change, an assignment, a new task); the
 * `TaskSummaryProvider` listens and refetches the counts behind the navigation
 * badge and the queue's chips at once, instead of waiting for its next poll.
 *
 * It is a module-level bus rather than a context because the services that
 * fire it are plain functions with no React tree to reach into.
 */

type Listener = () => void

const listeners = new Set<Listener>()

/**
 * Registers a listener for task changes and returns the function that removes
 * it again.
 */
export function subscribeTaskChanges(listener: Listener): () => void {
  listeners.add(listener)
  return () => {
    listeners.delete(listener)
  }
}

/** Tells every listener the work queue changed. Safe to call with no listeners. */
export function notifyTaskChanged(): void {
  for (const listener of listeners) {
    listener()
  }
}
