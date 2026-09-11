import { render, screen } from '@testing-library/react'
import { I18nextProvider } from 'react-i18next'
import { MemoryRouter } from 'react-router-dom'
import { beforeEach, describe, expect, it } from 'vitest'

import i18n from '../../i18n'
import type { RemainingWork, VideoStatus } from '../../services/system'

import { RemainingWorkPanel } from './RemainingWorkPanel'

function remaining(overrides: Partial<RemainingWork> = {}): RemainingWork {
  return {
    faces_unassigned: 812,
    clusters: 41,
    photos_without_taken_at: 0,
    photos_without_gps: 6104,
    photos_without_place: 0,
    photos_without_ocr: 233,
    duplicate_markers: 0,
    videos_without_streaming: 40,
    duplicates: { configured: true, available: true, groups: 17 },
    ...overrides,
  }
}

const VIDEO: VideoStatus = {
  streaming_enabled: true,
  videos: 145,
  streamable: 105,
  missing: 40,
  encode_queued: 12,
  encode_running: 1,
  encode_failed: 3,
  not_scheduled: 24,
  renditions: 130,
}

function renderPanel(work: RemainingWork) {
  return render(
    <MemoryRouter>
      <I18nextProvider i18n={i18n}>
        <RemainingWorkPanel remaining={work} video={VIDEO} />
      </I18nextProvider>
    </MemoryRouter>,
  )
}

beforeEach(async () => {
  await i18n.changeLanguage('en')
})

describe('RemainingWorkPanel', () => {
  it('reads every backlog as work waiting, on the page’s shared scale', () => {
    renderPanel(remaining())

    for (const key of [
      'faces-unassigned',
      'clusters',
      'without-gps',
      'without-ocr',
      'videos-without-streaming',
      'duplicates',
    ]) {
      expect(screen.getByTestId(`tile-${key}`)).toHaveClass('kk-count--queued')
    }
    // Nothing in this section is a failure, so nothing here is danger.
    expect(document.querySelector('.kk-count--failed')).toBeNull()
  })

  it('lets a cleared backlog go quiet — it is the goal, not a warning', () => {
    renderPanel(remaining())

    for (const key of ['without-taken-at', 'without-place', 'duplicate-markers']) {
      const tile = screen.getByTestId(`tile-${key}`)
      expect(tile).toHaveTextContent('0')
      expect(tile).toHaveClass('text-secondary')
      expect(tile.className).not.toMatch(/kk-count--(queued|running|failed)/)
    }
  })

  it('keeps the duplicates tile’s "no answer yet" reading uncoloured', () => {
    renderPanel(remaining({ duplicates: { configured: true, available: false, groups: 0 } }))

    const tile = screen.getByTestId('tile-duplicates')
    expect(tile).toHaveTextContent('—')
    expect(tile).toHaveClass('text-secondary')
    expect(screen.getByText('Scanning in the background…')).toBeInTheDocument()
  })
})
