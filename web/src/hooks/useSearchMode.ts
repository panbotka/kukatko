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
  /**
   * Whether a semantic request is worth sending. False only when the instance
   * has actually said it cannot answer one — never merely because the flags have
   * not arrived, which is a different thing and must not be reported as a
   * missing feature.
   */
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
 *
 * Until the flags are known the requested mode is sent unchanged. Downgrading on
 * a guess is the worse error of the two: it costs the reader the search they
 * asked for and, because the page explains the downgrade, tells them a feature
 * is unavailable on no evidence — which is exactly what an unauthenticated
 * capabilities request used to do. The backend degrades and reports it in the
 * reply if the sidecar really is down, so the truth still reaches the page; the
 * only cost is one request that may wait for the sidecar timeout.
 */
export function useSearchMode(requested: SearchMode): SearchModeState {
  const { semantic_search: semanticSearch, known } = useCapabilities()
  const semanticAvailable = !known || semanticSearch
  const mode = effectiveSearchMode(requested, semanticAvailable)
  return { mode, semanticAvailable, downgraded: mode !== requested }
}
