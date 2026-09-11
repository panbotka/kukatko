import { render, screen } from '@testing-library/react'
import { I18nextProvider } from 'react-i18next'
import { beforeEach, describe, expect, it } from 'vitest'

import i18n from '../../i18n'
import type { VideoStatus } from '../../services/system'

import { VideoEncodingPanel } from './VideoEncodingPanel'

/** A library of 145 clips, most of them encoded, with a queue that is moving. */
function video(overrides: Partial<VideoStatus> = {}): VideoStatus {
  return {
    streaming_enabled: true,
    videos: 145,
    streamable: 105,
    missing: 40,
    encode_queued: 12,
    encode_running: 1,
    encode_failed: 3,
    not_scheduled: 24,
    renditions: 130,
    oldest_queued_at: new Date(Date.now() - 3 * 60 * 60 * 1000).toISOString(),
    ...overrides,
  }
}

function renderPanel(status: VideoStatus) {
  return render(
    <I18nextProvider i18n={i18n}>
      <VideoEncodingPanel video={status} />
    </I18nextProvider>,
  )
}

beforeEach(async () => {
  await i18n.changeLanguage('en')
})

describe('VideoEncodingPanel', () => {
  it('counts videos, not jobs, and breaks the unencoded ones down by what they wait for', () => {
    renderPanel(video())

    expect(screen.getByRole('heading', { name: 'Video streaming' })).toBeInTheDocument()
    expect(screen.getByTestId('video-videos')).toHaveTextContent('145')
    expect(screen.getByTestId('video-streamable')).toHaveTextContent('105')
    expect(screen.getByTestId('video-missing')).toHaveTextContent('40')
    expect(screen.getByTestId('video-running')).toHaveTextContent('1')
    expect(screen.getByTestId('video-queued')).toHaveTextContent('12')
    expect(screen.getByTestId('video-failed')).toHaveTextContent('3')
    // The state the queue itself cannot show: nothing scheduled, nothing failed.
    expect(screen.getByTestId('video-not-scheduled')).toHaveTextContent('24')
    expect(screen.getByTestId('video-renditions')).toHaveTextContent('130')
    // How long the queue has been standing still, which a depth alone never says.
    expect(screen.getByTestId('video-oldest-wait')).toHaveTextContent(
      'The longest-waiting encode has been waiting 3h ago.',
    )
  })

  it('colours each row by what it counts, on the page’s shared scale', () => {
    renderPanel(video())

    // Waiting work — the backlog itself and the clips nobody scheduled.
    expect(screen.getByTestId('video-missing')).toHaveClass('kk-count--queued')
    expect(screen.getByTestId('video-queued')).toHaveClass('kk-count--queued')
    expect(screen.getByTestId('video-not-scheduled')).toHaveClass('kk-count--queued')
    // Work in flight, and visibly so.
    expect(screen.getByTestId('video-running')).toHaveClass('kk-count--running')
    expect(screen.getByTestId('video-running')).toHaveClass('kk-count--running')
    // A failure is danger, and never says so with colour alone.
    const failed = screen.getByTestId('video-failed')
    expect(failed).toHaveClass('kk-count--failed')
    expect(failed.querySelector('.bi-exclamation-triangle')).not.toBeNull()
    // A plain fact about the library takes no colour at all.
    for (const key of ['video-videos', 'video-streamable', 'video-renditions']) {
      const cell = screen.getByTestId(key)
      expect(cell.className).toBe('text-end')
    }
  })

  it('leaves every zero muted and uncoloured, warning glyph included', () => {
    renderPanel(
      video({
        missing: 0,
        encode_queued: 0,
        encode_running: 0,
        encode_failed: 0,
        not_scheduled: 0,
        streamable: 145,
        oldest_queued_at: undefined,
      }),
    )

    for (const key of ['video-missing', 'video-queued', 'video-running', 'video-not-scheduled']) {
      const cell = screen.getByTestId(key)
      expect(cell).toHaveClass('text-secondary')
      expect(cell.className).not.toMatch(/kk-count--(queued|running|failed)/)
      expect(cell).not.toHaveClass('kk-count--running')
    }
    // Nothing failed, so no danger and no glyph.
    const failed = screen.getByTestId('video-failed')
    expect(failed).toHaveClass('text-secondary')
    expect(failed).not.toHaveClass('kk-count--failed')
    expect(failed.querySelector('.bi-exclamation-triangle')).toBeNull()
    // Nothing is queued, so there is no wait to report.
    expect(screen.queryByTestId('video-oldest-wait')).not.toBeInTheDocument()
  })

  it('says streaming is switched off instead of reporting the library as a backlog', () => {
    renderPanel(video({ streaming_enabled: false }))

    expect(screen.getByText('Switched off')).toBeInTheDocument()
    expect(screen.getByTestId('video-disabled')).toHaveTextContent(
      /switched off, so nothing is being encoded. All 145 videos/,
    )
    // None of the counts is shown: with nothing ever encoded they are not work.
    expect(screen.queryByTestId('video-missing')).not.toBeInTheDocument()
    expect(screen.queryByTestId('video-not-scheduled')).not.toBeInTheDocument()
  })

  it('reads correctly with no videos in the library, switched on or off', () => {
    const empty = {
      videos: 0,
      streamable: 0,
      missing: 0,
      encode_queued: 0,
      encode_running: 0,
      encode_failed: 0,
      not_scheduled: 0,
      renditions: 0,
      oldest_queued_at: undefined,
    }
    const { unmount } = renderPanel(video(empty))
    expect(screen.getByTestId('video-empty')).toHaveTextContent('There is no video in the library')
    expect(screen.queryByTestId('video-videos')).not.toBeInTheDocument()
    unmount()

    renderPanel(video({ ...empty, streaming_enabled: false }))
    // Not "all 0 videos play as the original file": the honest sentence for an
    // instance that has neither streaming nor videos.
    expect(screen.getByTestId('video-disabled')).toHaveTextContent(
      'switched off, and there is no video in the library anyway',
    )
  })

  it('renders the section in Czech too', async () => {
    await i18n.changeLanguage('cs')
    renderPanel(video())

    expect(screen.getByRole('heading', { name: 'Video ke streamování' })).toBeInTheDocument()
    expect(screen.getByText('Bez plynulé verze')).toBeInTheDocument()
    await i18n.changeLanguage('en')
  })
})
