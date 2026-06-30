import { fireEvent, render, screen } from '@testing-library/react'
import { I18nextProvider } from 'react-i18next'
import { beforeEach, describe, expect, it } from 'vitest'

import i18n from '../../i18n'

import { VideoPlayer } from './VideoPlayer'

function renderPlayer() {
  return render(
    <I18nextProvider i18n={i18n}>
      <VideoPlayer
        uid="ph1"
        title="Clip"
        poster="/poster.jpg"
        downloadHref="/api/v1/photos/ph1/download?original=true"
      />
    </I18nextProvider>,
  )
}

beforeEach(async () => {
  await i18n.changeLanguage('en')
})

describe('VideoPlayer', () => {
  it('renders a native video element streaming the range endpoint', () => {
    const { container } = renderPlayer()
    const video = container.querySelector('video')
    expect(video).not.toBeNull()
    expect(video?.getAttribute('src')).toContain('/photos/ph1/video')
    expect(video?.hasAttribute('controls')).toBe(true)
    expect(video?.getAttribute('poster')).toBe('/poster.jpg')
  })

  it('falls back to a download link when the codec cannot be played', () => {
    const { container } = renderPlayer()
    const video = container.querySelector('video')
    if (video === null) {
      throw new Error('expected a video element')
    }
    fireEvent.error(video)

    expect(screen.getByText('This video cannot be played in your browser.')).toBeInTheDocument()
    // react-bootstrap renders the styled anchor with role="button".
    const link = screen.getByRole('button', { name: 'Download the video' })
    expect(link).toHaveAttribute('href', '/api/v1/photos/ph1/download?original=true')
  })
})
