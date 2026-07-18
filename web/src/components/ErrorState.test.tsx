import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { I18nextProvider } from 'react-i18next'
import { describe, expect, it, vi } from 'vitest'

import i18n from '../i18n'

import { ErrorState } from './ErrorState'

/** Renders the component with the real i18n instance so `t()` resolves. */
function renderState(ui: React.ReactElement) {
  return render(<I18nextProvider i18n={i18n}>{ui}</I18nextProvider>)
}

describe('ErrorState', () => {
  it('renders the message as an alert with the hint', () => {
    renderState(<ErrorState title="Nepodařilo se načíst" hint="Zkontroluj připojení." />)

    const root = screen.getByRole('alert')
    expect(root).toHaveTextContent('Nepodařilo se načíst')
    expect(screen.getByText('Zkontroluj připojení.')).toBeInTheDocument()
  })

  it('omits the hint when none is given', () => {
    renderState(<ErrorState title="Chyba" />)

    expect(screen.getByTestId('error-state').querySelector('.kk-empty-state__hint')).toBeNull()
  })

  it('renders the warning glyph hidden from assistive technology', () => {
    const { container } = renderState(<ErrorState title="Chyba" />)

    const well = container.querySelector('.kk-empty-state__icon')
    expect(well).not.toBeNull()
    expect(well).toHaveAttribute('aria-hidden', 'true')
    expect(well?.querySelector('.bi-exclamation-triangle')).not.toBeNull()
  })

  it('shows a Retry button that calls onRetry, labelled from the shared key', async () => {
    const onRetry = vi.fn()
    const user = userEvent.setup()
    renderState(<ErrorState title="Chyba" onRetry={onRetry} />)

    const button = screen.getByRole('button', { name: 'Zkusit znovu' })
    await user.click(button)
    expect(onRetry).toHaveBeenCalledTimes(1)
  })

  it('lets the caller override the retry label', () => {
    renderState(<ErrorState title="Chyba" onRetry={() => undefined} retryLabel="Načíst znovu" />)

    expect(screen.getByRole('button', { name: 'Načíst znovu' })).toBeInTheDocument()
  })

  it('renders no button when neither a retry nor an action is given', () => {
    renderState(<ErrorState title="Chyba" />)

    expect(screen.queryByRole('button')).not.toBeInTheDocument()
  })

  it('renders a custom action alongside the message', () => {
    renderState(<ErrorState title="Chyba" action={<a href="/back">Zpět</a>} />)

    expect(screen.getByRole('link', { name: 'Zpět' })).toBeInTheDocument()
  })

  it('applies the compact variant and merges extra classes', () => {
    renderState(<ErrorState title="Chyba" size="sm" className="mt-2" />)

    const root = screen.getByTestId('error-state')
    expect(root).toHaveClass(
      'kk-empty-state',
      'kk-empty-state--error',
      'kk-empty-state--sm',
      'kk-appear',
      'mt-2',
    )
  })
})
