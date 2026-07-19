import { createContext, useContext } from 'react'

import { type Capabilities } from '../services/capabilities'

/**
 * The safe default feature flags: everything off. A component that reads
 * capabilities before the first fetch resolves — or one rendered outside a
 * {@link CapabilitiesProvider} (e.g. in a focused unit test) — conservatively
 * hides optional affordances rather than advertising a feature that may not be
 * available. Because the flags are purely presentational (full-text search works
 * regardless), a wrong default only ever hides a hint, never breaks a flow.
 */
export const CAPABILITIES_DEFAULT: Capabilities = { semantic_search: false }

/**
 * Provides the current instance feature flags. Unlike the auth context this one
 * carries a non-null default and its hook never throws when read outside a
 * provider: capabilities are progressive enhancement, so a missing provider must
 * degrade gracefully to "all features off" instead of crashing the tree.
 */
export const CapabilitiesContext = createContext<Capabilities>(CAPABILITIES_DEFAULT)

/**
 * Returns the current instance feature flags from the nearest
 * {@link CapabilitiesProvider}, or the safe {@link CAPABILITIES_DEFAULT} when
 * there is none.
 */
export function useCapabilities(): Capabilities {
  return useContext(CapabilitiesContext)
}
