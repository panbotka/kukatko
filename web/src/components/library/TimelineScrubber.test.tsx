import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { I18nextProvider } from 'react-i18next'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import i18n from '../../i18n'
import { type PhotoListParams, type Timeline } from '../../services/photos'

import { TimelineScrubber } from './TimelineScrubber'

// Only the network call is faked; the component's positioning/highlight logic
// runs for real.
vi.mock('../../services/photos', async (importOriginal) => {
  const actual = await importOriginal<typeof import('../../services/photos')>()
  return { ...actual, fetchTimeline: vi.fn() }
})

const { fetchTimeline } = await import('../../services/photos')
const fetchMock = vi.mocked(fetchTimeline)

const TIMELINE: Timeline = {
  buckets: [
    { year: 2026, month: 2, count: 3, cumulative: 0 },
    { year: 2026, month: 1, count: 5, cumulative: 3 },
  ],
  total: 8,
}

function renderScrubber(props: {
  params?: PhotoListParams
  activeIndex?: number
  onJump?: (index: number) => void
}) {
  return render(
    <I18nextProvider i18n={i18n}>
      <TimelineScrubber
        params={props.params ?? { sort: 'newest' }}
        activeIndex={props.activeIndex ?? 0}
        onJump={props.onJump ?? vi.fn()}
      />
    </I18nextProvider>,
  )
}

beforeEach(async () => {
  await i18n.changeLanguage('en')
  fetchMock.mockReset()
})

afterEach(() => {
  vi.restoreAllMocks()
})

describe('TimelineScrubber', () => {
  it('renders a tick per bucket and clicking one jumps to its cumulative index', async () => {
    fetchMock.mockResolvedValue(TIMELINE)
    const onJump = vi.fn()
    const user = userEvent.setup()
    renderScrubber({ onJump })

    const jan = await screen.findByRole('button', { name: 'Jump to Jan 2026' })
    expect(screen.getByRole('button', { name: 'Jump to Feb 2026' })).toBeInTheDocument()

    await user.click(jan)
    expect(onJump).toHaveBeenCalledWith(3)
  })

  it('reflects the active filters, refetching when the params change', async () => {
    fetchMock.mockResolvedValue(TIMELINE)
    const { rerender } = renderScrubber({ params: { sort: 'newest', camera: 'Canon' } })

    await waitFor(() => {
      expect(fetchMock).toHaveBeenCalled()
    })
    expect(fetchMock.mock.calls[0][0]).toMatchObject({ camera: 'Canon' })

    rerender(
      <I18nextProvider i18n={i18n}>
        <TimelineScrubber
          params={{ sort: 'newest', camera: 'Nikon' }}
          activeIndex={0}
          onJump={vi.fn()}
        />
      </I18nextProvider>,
    )

    await waitFor(() => {
      const last = fetchMock.mock.calls[fetchMock.mock.calls.length - 1][0]
      expect(last).toMatchObject({ camera: 'Nikon' })
    })
  })

  it('highlights the month containing the current scroll range', async () => {
    fetchMock.mockResolvedValue(TIMELINE)
    const { rerender } = renderScrubber({ activeIndex: 0 })

    // Range starts at index 0 → the newest bucket (Feb) is active.
    const feb = await screen.findByRole('button', { name: 'Jump to Feb 2026' })
    expect(feb).toHaveAttribute('aria-current', 'true')
    expect(screen.getByRole('button', { name: 'Jump to Jan 2026' })).not.toHaveAttribute(
      'aria-current',
      'true',
    )

    // Scrolling to index 5 lands inside the second bucket (cumulative 3..7).
    rerender(
      <I18nextProvider i18n={i18n}>
        <TimelineScrubber params={{ sort: 'newest' }} activeIndex={5} onJump={vi.fn()} />
      </I18nextProvider>,
    )

    await waitFor(() => {
      expect(screen.getByRole('button', { name: 'Jump to Jan 2026' })).toHaveAttribute(
        'aria-current',
        'true',
      )
    })
    expect(screen.getByRole('button', { name: 'Jump to Feb 2026' })).not.toHaveAttribute(
      'aria-current',
      'true',
    )
  })

  it('renders nothing when the timeline has no buckets', async () => {
    fetchMock.mockResolvedValue({ buckets: [], total: 0 })
    renderScrubber({})

    await waitFor(() => {
      expect(fetchMock).toHaveBeenCalled()
    })
    expect(screen.queryByRole('navigation')).not.toBeInTheDocument()
  })
})
