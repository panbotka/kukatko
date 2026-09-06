import { describe, expect, it } from 'vitest'

import { readViewerPanel, writeViewerPanel } from './viewerPanel'

describe('readViewerPanel', () => {
  it('reads each of the drawer’s three views', () => {
    for (const panel of ['info', 'faces', 'edits'] as const) {
      expect(readViewerPanel(new URLSearchParams(`panel=${panel}`))).toBe(panel)
    }
  })

  it('reports no panel when none is named', () => {
    expect(readViewerPanel(new URLSearchParams(''))).toBeNull()
    expect(readViewerPanel(new URLSearchParams('sort=oldest'))).toBeNull()
  })

  it('opens the plain photo for a value it does not know', () => {
    // A hand-edited or future URL must not break the viewer.
    expect(readViewerPanel(new URLSearchParams('panel=nonsense'))).toBeNull()
    expect(readViewerPanel(new URLSearchParams('panel='))).toBeNull()
  })

  it('still understands the legacy info=1 link', () => {
    expect(readViewerPanel(new URLSearchParams('sort=oldest&info=1'))).toBe('info')
    // Only the flag as it was ever written; anything else is not the flag.
    expect(readViewerPanel(new URLSearchParams('info=0'))).toBeNull()
  })

  it('lets the named panel win over the legacy flag', () => {
    expect(readViewerPanel(new URLSearchParams('info=1&panel=faces'))).toBe('faces')
  })
})

describe('writeViewerPanel', () => {
  it('names the open panel and keeps the rest of the query', () => {
    const next = writeViewerPanel(new URLSearchParams('sort=oldest'), 'edits')
    expect(next.get('panel')).toBe('edits')
    expect(next.get('sort')).toBe('oldest')
  })

  it('drops the parameter when no panel is open', () => {
    const next = writeViewerPanel(new URLSearchParams('sort=oldest&panel=info'), null)
    expect(next.has('panel')).toBe(false)
    expect(next.get('sort')).toBe('oldest')
  })

  it('rewrites a legacy link to the new form', () => {
    const next = writeViewerPanel(new URLSearchParams('info=1&sort=oldest'), 'faces')
    expect(next.has('info')).toBe(false)
    expect(next.get('panel')).toBe('faces')
    expect(next.toString()).toBe('sort=oldest&panel=faces')
  })

  it('leaves its input untouched', () => {
    const params = new URLSearchParams('info=1')
    writeViewerPanel(params, 'info')
    expect(params.toString()).toBe('info=1')
  })
})
