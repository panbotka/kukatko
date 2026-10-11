import { render, screen } from '@testing-library/react'
import { describe, expect, it } from 'vitest'

import { declarations, readCss, ruleBody } from '../test/css'

import { NarrowPage } from './NarrowPage'

describe('NarrowPage', () => {
  it('wraps the page in the shared narrow column', () => {
    render(
      <NarrowPage>
        <h1>Můj účet</h1>
      </NarrowPage>,
    )

    const column = screen.getByTestId('narrow-page')
    expect(column).toHaveClass('kk-page-narrow')
    expect(column).toContainElement(screen.getByRole('heading', { level: 1 }))
  })

  it('caps the column at a reading measure and centres it in the container', () => {
    // jsdom applies no stylesheet, so this reads the shipped one.
    const body = ruleBody(readCss('src/styles/app.css'), /\.kk-page-narrow\s*(?=\{)/)
    expect(body).toBeDefined()
    const rule = declarations(body ?? '')
    expect(rule.get('max-width')).toBe('48rem')
    expect(rule.get('margin-inline')).toBe('auto')
  })
})
