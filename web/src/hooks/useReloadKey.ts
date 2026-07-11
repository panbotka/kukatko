import { useCallback, useState } from 'react'

/** A reload key and the callback that bumps it; see {@link useReloadKey}. */
export type UseReloadKeyResult = readonly [key: string, reload: () => void]

/**
 * A counter rendered as a string, to be handed to a photo-list hook as its
 * `reloadKey`: calling `reload()` changes the key, which resets the list and
 * refetches it from the first page.
 *
 * Every page that mutates the photos it is showing — a bulk edit, a removal from
 * an album — needs exactly this, because the mutation can change what the current
 * filters and scope match, and the loaded pages would otherwise keep showing the
 * pre-mutation list. `reload` is stable, so it can be passed straight to
 * `useBulkEdit({ onEdited })`.
 */
export function useReloadKey(): UseReloadKeyResult {
  const [key, setKey] = useState('0')
  const reload = useCallback(() => {
    setKey((k) => String(Number(k) + 1))
  }, [])
  return [key, reload]
}
