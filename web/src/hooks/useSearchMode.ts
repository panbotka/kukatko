import { useCapabilities } from '../capabilities/CapabilitiesContext'
import { effectiveSearchMode } from '../lib/searchView'
import { type SearchMode } from '../services/photos'

/** What {@link useSearchMode} reports about the mode a search will really use. */
export interface SearchModeState {
  /**
   * The mode to send. Equals the requested one whenever it can be served; a
   * semantic or hybrid request falls back to `fulltext` while the embeddings
   * sidecar is unreachable.
   */
  mode: SearchMode
  /** Whether the instance can currently answer embedding-backed searches. */
  semanticAvailable: boolean
  /**
   * True when the requested mode was replaced by `fulltext`. Pages use it to say
   * so *before* the search runs, rather than only reacting to the server's own
   * `degraded` flag once the results are back.
   */
  downgraded: boolean
}

/**
 * Resolves the search mode to use from the requested one and the instance
 * capabilities. The embeddings box is offline most of the time here; a semantic
 * or hybrid request then spends the sidecar timeout before the backend degrades
 * it to full-text and returns results that were available all along. Since the
 * capability flag already says the sidecar is unreachable, this asks for
 * full-text straight away — no request that can hit the sidecar timeout is sent.
 *
 * Capabilities refresh in the background, so a box coming back online flips
 * `mode` back to the requested one within about a minute and the search re-runs.
 */
export function useSearchMode(requested: SearchMode): SearchModeState {
  const { semantic_search: semanticAvailable } = useCapabilities()
  const mode = effectiveSearchMode(requested, semanticAvailable)
  return { mode, semanticAvailable, downgraded: mode !== requested }
}
