import { renderHook } from '@testing-library/react'
import { type ReactNode } from 'react'
import { describe, expect, it } from 'vitest'

import { CapabilitiesContext } from '../capabilities/CapabilitiesContext'
import { type SearchMode } from '../services/photos'

import { useSearchMode } from './useSearchMode'

/** Renders the hook under an instance that does or does not offer semantic search. */
function render(requested: SearchMode, semanticSearch: boolean) {
  const wrapper = ({ children }: { children: ReactNode }) => (
    <CapabilitiesContext.Provider value={{ semantic_search: semanticSearch }}>
      {children}
    </CapabilitiesContext.Provider>
  )
  return renderHook(() => useSearchMode(requested), { wrapper }).result.current
}

describe('useSearchMode', () => {
  it('passes the requested mode through while the sidecar is reachable', () => {
    expect(render('semantic', true)).toEqual({
      mode: 'semantic',
      semanticAvailable: true,
      downgraded: false,
    })
  })

  it('downgrades hybrid to full-text while it is not', () => {
    expect(render('hybrid', false)).toEqual({
      mode: 'fulltext',
      semanticAvailable: false,
      downgraded: true,
    })
  })

  it('does not call full-text a downgrade, so no page claims one', () => {
    // Full-text never needed the sidecar; saying it was downgraded would put a
    // "showing text results instead" notice on a search that asked for exactly
    // those results.
    expect(render('fulltext', false)).toEqual({
      mode: 'fulltext',
      semanticAvailable: false,
      downgraded: false,
    })
  })

  it('reports all-off outside a provider, so a missing one cannot cost a timeout', () => {
    // CAPABILITIES_DEFAULT is semantic_search: false; the conservative answer is
    // a search that returns something over one that waits for a box that may not
    // be there.
    expect(renderHook(() => useSearchMode('hybrid')).result.current.mode).toBe('fulltext')
  })
})
