import { renderHook } from '@testing-library/react'
import { type ReactNode } from 'react'
import { describe, expect, it } from 'vitest'

import { CapabilitiesContext } from '../capabilities/CapabilitiesContext'
import { type SearchMode } from '../services/photos'

import { useSearchMode } from './useSearchMode'

/**
 * Renders the hook under an instance that does or does not offer semantic search.
 * The flags are marked known, which is what makes "off" mean the instance said so
 * rather than that nobody has asked it yet.
 */
function render(requested: SearchMode, semanticSearch: boolean, known = true) {
  const wrapper = ({ children }: { children: ReactNode }) => (
    <CapabilitiesContext.Provider value={{ semantic_search: semanticSearch, known }}>
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

  it('sends the requested mode while the flags are still unknown', () => {
    // The flags start all-off and only a reply makes them mean anything. Reading
    // that blank as "the sidecar is down" is what made a capabilities request
    // answered 401 tell the reader content search was unavailable while it was
    // running. Unknown must therefore behave as "go ahead and ask".
    expect(render('hybrid', false, false)).toEqual({
      mode: 'hybrid',
      semanticAvailable: true,
      downgraded: false,
    })
  })

  it('claims no downgrade on an unknown instance, so no page invents an outage', () => {
    // The specific regression: the banner explaining the fallback keys off
    // `downgraded`, so an instance nobody could ask must not set it.
    expect(render('semantic', false, false).downgraded).toBe(false)
  })

  it('reports nothing known outside a provider, and so downgrades nothing', () => {
    // CAPABILITIES_DEFAULT carries known: false. A missing provider is a test
    // artefact rather than a real state, and it must not fabricate an outage
    // either; the backend still reports its own fallback in the reply.
    expect(renderHook(() => useSearchMode('hybrid')).result.current).toEqual({
      mode: 'hybrid',
      semanticAvailable: true,
      downgraded: false,
    })
  })
})
