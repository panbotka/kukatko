import { render } from '@testing-library/react'
import { beforeEach, describe, expect, it } from 'vitest'

import { clearBlurPlaceholderCache } from '../lib/blurPlaceholder'
import { STUB_CANVAS_DATA_URL, stubBlurCanvas } from '../test/canvas'
import { declarations, readCss, ruleBody } from '../test/css'

import { BlurPlaceholder } from './BlurPlaceholder'

/** A real BlurHash of a real photograph (the woltapp reference string). */
const HASH = 'LEHV6nWB2yk8pyo0adR*.7kCMdnj'

describe('BlurPlaceholder', () => {
  beforeEach(() => {
    clearBlurPlaceholderCache()
  })

  it('paints the decoded hash as a decorative layer over the whole box', () => {
    stubBlurCanvas()

    render(<BlurPlaceholder hash={HASH} />)

    const layer = document.querySelector('.kk-media-blur')
    expect(layer).toBeInTheDocument()
    expect(layer).toHaveAttribute('aria-hidden', 'true')
    expect(layer).toHaveStyle({ backgroundImage: `url("${STUB_CANVAS_DATA_URL}")` })
  })

  it('renders nothing for a photo that carries no hash, leaving the neutral box', () => {
    stubBlurCanvas()

    const { container } = render(<BlurPlaceholder />)

    expect(container).toBeEmptyDOMElement()
  })

  it('renders nothing for a hash that cannot be decoded', () => {
    stubBlurCanvas()

    const { container } = render(<BlurPlaceholder hash="nope" />)

    expect(container).toBeEmptyDOMElement()
  })

  it('takes the caller class and style overrides', () => {
    stubBlurCanvas()

    render(<BlurPlaceholder hash={HASH} className="rounded-circle" style={{ opacity: 0.5 }} />)

    const layer = document.querySelector('.kk-media-blur')
    expect(layer).toHaveClass('kk-media-blur', 'rounded-circle')
    expect(layer).toHaveStyle({ opacity: '0.5' })
  })
})

describe('the blur layer never moves the layout', () => {
  it('is an absolutely positioned, inert layer stretched over its parent box', () => {
    // The whole no-layout-shift promise is these declarations: a layer taken out
    // of flow and pinned to a box the caller has already sized means the tile's
    // geometry cannot depend on anything about the image or its placeholder.
    const body = ruleBody(readCss('src/styles/app.css'), /\.kk-media-blur\s*(?=\{)/)
    expect(body).toBeDefined()

    const rule = declarations(body ?? '')
    expect(rule.get('position')).toBe('absolute')
    expect(rule.get('inset')).toBe('0')
    // Stretched, not cropped: a blur has no detail to lose and its colours must
    // stay where the photograph's are.
    expect(rule.get('background-size')).toBe('100% 100%')
    expect(rule.get('pointer-events')).toBe('none')
  })
})
