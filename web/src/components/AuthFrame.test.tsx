import { render, screen, within } from '@testing-library/react'
import { I18nextProvider } from 'react-i18next'
import { describe, expect, it } from 'vitest'

import i18n from '../i18n'

import { AuthFrame } from './AuthFrame'

function renderFrame(width?: 'narrow' | 'wide') {
  return render(
    <I18nextProvider i18n={i18n}>
      <AuthFrame title="Přihlášení" width={width}>
        <p>form goes here</p>
      </AuthFrame>
    </I18nextProvider>,
  )
}

describe('AuthFrame', () => {
  it('puts the app name above the card and keeps the card title the only h1', () => {
    renderFrame()

    const wordmark = screen.getByTestId('auth-wordmark')
    expect(wordmark).toHaveTextContent('Kukátko')
    // The name is branding, not a heading: the route's single h1 stays the card's.
    expect(within(wordmark).queryByRole('heading')).toBeNull()
    const headings = screen.getAllByRole('heading', { level: 1 })
    expect(headings).toHaveLength(1)
    expect(headings[0]).toHaveTextContent('Přihlášení')

    // The wordmark comes first in the document, the card's content after it.
    const body = screen.getByText('form goes here')
    expect(wordmark.compareDocumentPosition(body) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy()
  })

  it('draws its mark inline and hides it from assistive technology', () => {
    renderFrame()

    const mark = screen.getByTestId('auth-wordmark').querySelector('svg')
    expect(mark).not.toBeNull()
    expect(mark).toHaveAttribute('aria-hidden', 'true')
    // No external asset: the mark is drawn, never loaded.
    expect(screen.getByTestId('auth-frame').querySelector('img')).toBeNull()
  })

  it('is wide by default and narrow on request', () => {
    const { unmount } = renderFrame()
    expect(
      screen.getByTestId('auth-frame').querySelector('.kk-auth-frame-column--wide'),
    ).not.toBeNull()
    unmount()

    renderFrame('narrow')
    expect(
      screen.getByTestId('auth-frame').querySelector('.kk-auth-frame-column--narrow'),
    ).not.toBeNull()
  })
})
