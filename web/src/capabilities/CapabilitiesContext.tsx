import { createContext, useContext } from 'react'

import { type Capabilities } from '../services/capabilities'

/**
 * The feature flags as the app holds them: the flags themselves plus whether
 * they were ever actually learned from the server.
 *
 * `known` is the difference between "this instance cannot answer content
 * searches" and "we have not managed to ask". Both used to look identical — the
 * flags start all-off and a failed fetch leaves them there — so a capabilities
 * request that never succeeded (an expired session answering 401, say) made the
 * UI announce that semantic search was unavailable when it was running fine.
 * Anything that tells a reader a feature is missing must key off `known`.
 */
export interface CapabilitiesState extends Capabilities {
  /** Whether a capabilities response was ever received; false until one is. */
  known: boolean
}

/**
 * The safe default: everything off, and nothing known yet. A component that
 * reads capabilities before the first fetch resolves — or one rendered outside a
 * {@link CapabilitiesProvider} (e.g. in a focused unit test) — conservatively
 * hides optional affordances rather than advertising a feature that may not be
 * available. Because the flags are purely presentational (full-text search works
 * regardless), a wrong default only ever hides a hint, never breaks a flow.
 */
export const CAPABILITIES_DEFAULT: CapabilitiesState = { semantic_search: false, known: false }

/**
 * Provides the current instance feature flags. Unlike the auth context this one
 * carries a non-null default and its hook never throws when read outside a
 * provider: capabilities are progressive enhancement, so a missing provider must
 * degrade gracefully to "all features off" instead of crashing the tree.
 */
export const CapabilitiesContext = createContext<CapabilitiesState>(CAPABILITIES_DEFAULT)

/**
 * Returns the current instance feature flags from the nearest
 * {@link CapabilitiesProvider}, or the safe {@link CAPABILITIES_DEFAULT} when
 * there is none.
 */
export function useCapabilities(): CapabilitiesState {
  return useContext(CapabilitiesContext)
}
