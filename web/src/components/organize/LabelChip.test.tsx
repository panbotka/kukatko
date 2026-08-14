import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { I18nextProvider } from 'react-i18next'
import { MemoryRouter } from 'react-router-dom'
import { beforeEach, describe, expect, it } from 'vitest'

import i18n from '../../i18n'
import { type LabelCount } from '../../services/organize'

import { LabelChip } from './LabelChip'

/** Builds a label row, with or without the cover the index derives for it. */
function label(overrides: Partial<LabelCount> = {}): LabelCount {
  return {
    uid: 'lb1',
    slug: 'sunset',
    name: 'sunset',
    priority: 0,
    review_enabled: true,
    created_at: '2026-01-01T00:00:00Z',
    updated_at: '2026-01-01T00:00:00Z',
    photo_count: 5,
    ...overrides,
  }
}

function renderChip(value: LabelCount) {
  return render(
    <I18nextProvider i18n={i18n}>
      <MemoryRouter>
        <LabelChip label={value} />
      </MemoryRouter>
    </I18nextProvider>,
  )
}

beforeEach(async () => {
  await i18n.changeLanguage('en')
})

describe('LabelChip', () => {
  it('leads with the photo standing for the label', () => {
    const { container } = renderChip(label({ cover_uid: 'ph9' }))

    const img = container.querySelector('img')
    expect(img).toHaveAttribute('src', '/api/v1/photos/ph9/thumb/tile_100')
    // Decoration: the link is already named after the label, and a cloud of a
    // hundred chips must not fetch a hundred images before it can be read.
    expect(img).toHaveAttribute('aria-hidden', 'true')
    expect(img).toHaveAttribute('alt', '')
    expect(img).toHaveAttribute('loading', 'lazy')
    // The picture changes nothing about how the chip announces itself.
    expect(screen.getByRole('link', { name: 'sunset, 5 photos' })).toBeInTheDocument()
  })

  it('stays a plain pill for a label with no photos', () => {
    const { container } = renderChip(label({ photo_count: 0 }))
    expect(container.querySelector('img')).toBeNull()
    expect(screen.getByRole('link', { name: /sunset/ })).toBeInTheDocument()
  })

  it('drops the preview when it fails to load', async () => {
    const { container } = renderChip(label({ cover_uid: 'ph9' }))

    const img = container.querySelector('img')
    if (img === null) {
      throw new Error('expected the chip to draw a preview')
    }
    fireEvent.error(img)
    await waitFor(() => {
      expect(container.querySelector('img')).toBeNull()
    })
    // The chip is still there, and still says what it is.
    expect(screen.getByRole('link', { name: /sunset/ })).toBeInTheDocument()
  })
})
