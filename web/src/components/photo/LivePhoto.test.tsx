import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { I18nextProvider } from 'react-i18next'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import i18n from '../../i18n'

import { LivePhoto } from './LivePhoto'

function renderLive() {
  return render(
    <I18nextProvider i18n={i18n}>
      <LivePhoto uid="ph1" title="Beach" poster="/still.jpg" />
    </I18nextProvider>,
  )
}

beforeEach(async () => {
  await i18n.changeLanguage('en')
  // jsdom does not implement media playback; stub it so start/stop can run.
  vi.spyOn(HTMLMediaElement.prototype, 'play').mockResolvedValue(undefined)
  vi.spyOn(HTMLMediaElement.prototype, 'pause').mockImplementation(() => undefined)
})

afterEach(() => {
  vi.restoreAllMocks()
})

describe('LivePhoto', () => {
  it('renders the still with a live badge', () => {
    renderLive()
    expect(screen.getByRole('img', { name: 'Beach' })).toHaveAttribute('src', '/still.jpg')
    expect(screen.getByText('Live')).toBeInTheDocument()
  })

  it('plays the motion clip on hover and pauses on leave', async () => {
    const playSpy = vi.spyOn(HTMLMediaElement.prototype, 'play').mockResolvedValue(undefined)
    const pauseSpy = vi
      .spyOn(HTMLMediaElement.prototype, 'pause')
      .mockImplementation(() => undefined)
    renderLive()

    const stage = screen.getByRole('button', { name: /Beach/ })
    fireEvent.mouseEnter(stage)
    await waitFor(() => {
      expect(playSpy).toHaveBeenCalled()
    })

    fireEvent.mouseLeave(stage)
    expect(pauseSpy).toHaveBeenCalled()
  })
})
