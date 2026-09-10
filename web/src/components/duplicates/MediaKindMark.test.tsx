import { render, screen } from '@testing-library/react'
import { I18nextProvider } from 'react-i18next'
import { beforeEach, describe, expect, it } from 'vitest'

import i18n from '../../i18n'

import { MediaKindMark } from './MediaKindMark'

function renderMark(props: { mediaType?: string; durationMs?: number }) {
  return render(
    <I18nextProvider i18n={i18n}>
      <MediaKindMark {...props} />
    </I18nextProvider>,
  )
}

beforeEach(async () => {
  await i18n.changeLanguage('en')
})

describe('MediaKindMark', () => {
  it('says a video is a video and how long it is', () => {
    renderMark({ mediaType: 'video', durationMs: 154_000 })

    const mark = screen.getByTestId('media-video')
    expect(mark).toHaveTextContent('Video')
    expect(mark).toHaveTextContent('2:34')
  })

  it('still says "video" when the duration is unknown', () => {
    renderMark({ mediaType: 'video' })

    const mark = screen.getByTestId('media-video')
    expect(mark).toHaveTextContent('Video')
    expect(mark).not.toHaveTextContent('0:00')
  })

  it('marks nothing on a still, so the exception stays visible', () => {
    renderMark({ mediaType: 'image' })
    expect(screen.queryByTestId('media-video')).not.toBeInTheDocument()
  })

  it('marks nothing on a live photo — it is a photograph that carries motion', () => {
    renderMark({ mediaType: 'live', durationMs: 2000 })
    expect(screen.queryByTestId('media-video')).not.toBeInTheDocument()
  })

  it('marks nothing when the media type has not loaded yet', () => {
    renderMark({})
    expect(screen.queryByTestId('media-video')).not.toBeInTheDocument()
  })
})
