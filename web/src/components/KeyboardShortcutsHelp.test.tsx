import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { I18nextProvider } from 'react-i18next'
import { beforeEach, describe, expect, it } from 'vitest'

import i18n from '../i18n'

import { KeyboardShortcutsHelp } from './KeyboardShortcutsHelp'

function renderHelp() {
  return render(
    <I18nextProvider i18n={i18n}>
      <KeyboardShortcutsHelp />
      <input aria-label="field" />
    </I18nextProvider>,
  )
}

beforeEach(async () => {
  await i18n.changeLanguage('en')
})

describe('KeyboardShortcutsHelp', () => {
  it('opens the help overlay when ? is pressed', async () => {
    renderHelp()
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()

    fireEvent.keyDown(document, { key: '?' })

    expect(await screen.findByRole('dialog')).toBeInTheDocument()
    expect(screen.getByText('Photo grid')).toBeInTheDocument()
    expect(screen.getByText('Photo detail')).toBeInTheDocument()
    expect(screen.getByText('Open the focused photo')).toBeInTheDocument()
  })

  it('documents the video-player keys and that the arrows still page photos', async () => {
    renderHelp()
    fireEvent.keyDown(document, { key: '?' })
    await screen.findByRole('dialog')

    expect(screen.getByText('Video player')).toBeInTheDocument()
    expect(screen.getByText('Skip 10 s back / forward')).toBeInTheDocument()
    expect(screen.getByText('Play / pause the video')).toBeInTheDocument()
    expect(screen.getByText(/Arrows keep paging between photos/)).toBeInTheDocument()
  })

  it('lists the selection context, keys and all', async () => {
    renderHelp()
    fireEvent.keyDown(document, { key: '?' })
    await screen.findByRole('dialog')

    expect(screen.getByText('Selecting photos')).toBeInTheDocument()
    expect(screen.getByText('Select / deselect the focused photo')).toBeInTheDocument()
    expect(screen.getByText('Shift + click selects a contiguous run of photos')).toBeInTheDocument()
    // Both selection keys are advertised, not just the vim-flavoured one.
    const row = screen.getByText('Select / deselect the focused photo').closest('tr')
    expect(row).not.toBeNull()
    expect(row?.textContent).toContain('Space')
    expect(row?.textContent).toContain('x')
  })

  it('is truthful about the keys that were never advertised before', async () => {
    renderHelp()
    fireEvent.keyDown(document, { key: '?' })
    await screen.findByRole('dialog')

    // The rating hotkeys, the slideshow and the review lightbox all exist in the
    // app; an overlay that omitted them would be a lie by silence.
    expect(screen.getByText('Set the star rating (0 clears it)')).toBeInTheDocument()
    expect(screen.getByText('Mark thumbs-up / thumbs-down / eye')).toBeInTheDocument()
    expect(screen.getByText('Slideshow')).toBeInTheDocument()
    expect(screen.getByText('Play / pause the show')).toBeInTheDocument()
    expect(screen.getByText('Review preview')).toBeInTheDocument()
    // …including the key that opens the overlay itself.
    expect(screen.getByText('Open this help')).toBeInTheDocument()
  })

  it('closes with Escape', async () => {
    const user = userEvent.setup()
    renderHelp()
    fireEvent.keyDown(document, { key: '?' })
    await screen.findByRole('dialog')

    await user.keyboard('{Escape}')
    await waitFor(() => {
      expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
    })
  })

  it('answers ? with no trigger of its own (the viewer mounts it bare)', async () => {
    render(
      <I18nextProvider i18n={i18n}>
        <KeyboardShortcutsHelp variant="bare" />
      </I18nextProvider>,
    )
    expect(screen.queryByRole('button', { name: 'Keyboard shortcuts' })).not.toBeInTheDocument()

    fireEvent.keyDown(document, { key: '?' })
    expect(await screen.findByRole('dialog')).toBeInTheDocument()
  })

  it('speaks Czech too', async () => {
    await i18n.changeLanguage('cs')
    renderHelp()
    fireEvent.keyDown(document, { key: '?' })
    await screen.findByRole('dialog')

    expect(screen.getByText('Klávesové zkratky')).toBeInTheDocument()
    expect(screen.getByText('Výběr fotek')).toBeInTheDocument()
    expect(screen.getByText('Detail fotky')).toBeInTheDocument()
  })

  it('opens the overlay from the keyboard-icon button', async () => {
    const user = userEvent.setup()
    renderHelp()
    await user.click(screen.getByRole('button', { name: 'Keyboard shortcuts' }))
    expect(await screen.findByRole('dialog')).toBeInTheDocument()
  })

  it('closes via the close button', async () => {
    const user = userEvent.setup()
    renderHelp()
    fireEvent.keyDown(document, { key: '?' })
    await screen.findByRole('dialog')

    await user.click(screen.getByRole('button', { name: 'Close' }))
    await waitFor(() => {
      expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
    })
  })

  it('does not open while typing in an input', () => {
    renderHelp()
    const input = screen.getByLabelText('field')
    input.focus()
    fireEvent.keyDown(input, { key: '?' })
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
  })
})
