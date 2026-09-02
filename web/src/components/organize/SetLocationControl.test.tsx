import { render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { I18nextProvider } from 'react-i18next'
import { beforeAll, beforeEach, describe, expect, it, vi } from 'vitest'

import { type UseBulkEditResult } from '../../hooks/useBulkEdit'
import i18n from '../../i18n'
import { type BulkResult } from '../../services/bulk'
import { expectLive, expectOff } from '../../test/reasoned'
import { ToastProvider } from '../toast/ToastProvider'

import { SetLocationControl } from './SetLocationControl'

// The map itself is covered by LocationPicker's own tests (and cannot lay itself
// out in jsdom); what matters here is the wiring around it, so the picker stands
// in as the one coordinate field it is controlled on.
vi.mock('../map/LocationPicker', () => ({
  LocationPicker: ({
    value,
    onChange,
    disabled,
  }: {
    value: string
    onChange: (text: string) => void
    disabled?: boolean
  }) => (
    <input
      aria-label="coordinates"
      value={value}
      disabled={disabled}
      onChange={(event) => {
        onChange(event.target.value)
      }}
    />
  ),
}))

vi.mock('../../services/bulk', async (importOriginal) => {
  const actual = await importOriginal<typeof import('../../services/bulk')>()
  return { ...actual, bulkUpdatePhotos: vi.fn(), fetchBulkLocationSummary: vi.fn() }
})

const { bulkUpdatePhotos, fetchBulkLocationSummary } = await import('../../services/bulk')
const bulkMock = vi.mocked(bulkUpdatePhotos)
const summaryMock = vi.mocked(fetchBulkLocationSummary)

/** A bulk result reporting what the backend did with the batch. */
function result(updated: number, skipped = 0): BulkResult {
  return { results: [], counts: { total: updated + skipped, updated, skipped, errored: 0 } }
}

/** A two-photo selection with spy callbacks, as a page would hand it over. */
function makeBulk(canBulkEdit = true): UseBulkEditResult {
  return {
    canBulkEdit,
    selection: {
      active: false,
      selected: new Set(['a', 'b']),
      count: 2,
      enable: vi.fn(),
      disable: vi.fn(),
      toggle: vi.fn(),
      toggleRange: vi.fn(),
      selectMany: vi.fn(),
      clear: vi.fn(),
    },
    photoUids: ['a', 'b'],
    gridSelection: undefined,
    editing: false,
    open: vi.fn(),
    close: vi.fn(),
    finish: vi.fn(),
  }
}

function renderControl(bulk: UseBulkEditResult) {
  return render(
    <I18nextProvider i18n={i18n}>
      <ToastProvider>
        <SetLocationControl bulk={bulk} />
      </ToastProvider>
    </I18nextProvider>,
  )
}

/** Opens the dialog and waits for the count of already-located photos. */
async function openDialog(user: ReturnType<typeof userEvent.setup>) {
  await user.click(screen.getByRole('button', { name: 'Set location' }))
  await waitFor(() => {
    expect(screen.queryByText(/Checking how many/)).not.toBeInTheDocument()
  })
}

/** The dialog's apply button (the trigger carries the same label). */
function applyButton(): HTMLElement {
  const dialog = screen.getByRole('dialog')
  return within(dialog).getByRole('button', { name: 'Set location' })
}

beforeAll(async () => {
  await i18n.changeLanguage('en')
})

beforeEach(() => {
  bulkMock.mockReset()
  summaryMock.mockReset()
})

describe('SetLocationControl', () => {
  it('fills only the photos without a location by default, saying how many that leaves', async () => {
    const user = userEvent.setup()
    summaryMock.mockResolvedValue({ total: 5, with_location: 2 })
    bulkMock.mockResolvedValue(result(3, 2))
    const bulk = makeBulk()
    renderControl(bulk)

    await openDialog(user)
    expect(
      screen.getByText('2 of the 5 selected photos already have a location.'),
    ).toBeInTheDocument()

    await user.type(screen.getByLabelText('coordinates'), '49.19522, 16.60796')
    await user.click(applyButton())

    await waitFor(() => {
      expect(bulkMock).toHaveBeenCalledWith(['a', 'b'], {
        set_location: { lat: 49.19522, lng: 16.60796, only_missing: true },
      })
    })
    // The selection is cleared and the page reloaded only after a success.
    await waitFor(() => {
      expect(bulk.finish).toHaveBeenCalled()
    })
    expect(
      await screen.findByText(
        'Location set for 3 photos; 2 with a location of their own were left unchanged.',
      ),
    ).toBeInTheDocument()
  })

  it('overwrites the photos that already have a location when asked to', async () => {
    const user = userEvent.setup()
    summaryMock.mockResolvedValue({ total: 5, with_location: 2 })
    bulkMock.mockResolvedValue(result(5))
    renderControl(makeBulk())

    await openDialog(user)
    await user.click(screen.getByLabelText('Overwrite them too'))
    await user.type(screen.getByLabelText('coordinates'), '49.19522, 16.60796')
    await user.click(applyButton())

    await waitFor(() => {
      expect(bulkMock).toHaveBeenCalledWith(['a', 'b'], {
        set_location: { lat: 49.19522, lng: 16.60796, only_missing: false },
      })
    })
  })

  it('offers no choice when nothing in the selection has a location yet', async () => {
    const user = userEvent.setup()
    summaryMock.mockResolvedValue({ total: 2, with_location: 0 })
    bulkMock.mockResolvedValue(result(2))
    renderControl(makeBulk())

    await openDialog(user)
    expect(
      screen.getByText('None of the 2 selected photos has a location yet.'),
    ).toBeInTheDocument()
    expect(screen.queryByLabelText('Overwrite them too')).not.toBeInTheDocument()

    await user.type(screen.getByLabelText('coordinates'), '49.19522, 16.60796')
    await user.click(applyButton())

    await waitFor(() => {
      expect(bulkMock).toHaveBeenCalledWith(['a', 'b'], {
        set_location: { lat: 49.19522, lng: 16.60796, only_missing: false },
      })
    })
  })

  it('still asks what to do when the count could not be read, and keeps existing locations', async () => {
    const user = userEvent.setup()
    summaryMock.mockRejectedValue(new Error('offline'))
    bulkMock.mockResolvedValue(result(2))
    renderControl(makeBulk())

    await openDialog(user)
    expect(screen.getByText(/Could not check how many/)).toBeInTheDocument()

    await user.type(screen.getByLabelText('coordinates'), '49.19522, 16.60796')
    await user.click(applyButton())

    await waitFor(() => {
      expect(bulkMock).toHaveBeenCalledWith(['a', 'b'], {
        set_location: { lat: 49.19522, lng: 16.60796, only_missing: true },
      })
    })
  })

  it('says why it cannot be applied until a place is picked', async () => {
    const user = userEvent.setup()
    summaryMock.mockResolvedValue({ total: 2, with_location: 0 })
    renderControl(makeBulk())

    await openDialog(user)
    expectOff(applyButton(), 'Pick a place on the map first.')

    await user.type(screen.getByLabelText('coordinates'), '49.19522, 16.60796')
    expectLive(applyButton())
  })

  it('keeps the selection after a failed apply, so it can be retried', async () => {
    const user = userEvent.setup()
    summaryMock.mockResolvedValue({ total: 2, with_location: 0 })
    bulkMock.mockRejectedValue(new Error('network down'))
    const bulk = makeBulk()
    renderControl(bulk)

    await openDialog(user)
    await user.type(screen.getByLabelText('coordinates'), '49.19522, 16.60796')
    await user.click(applyButton())

    await waitFor(() => {
      expect(bulkMock).toHaveBeenCalled()
    })
    expect(bulk.finish).not.toHaveBeenCalled()
    expect(screen.getByRole('dialog')).toBeInTheDocument()
  })

  it('is absent for a viewer, who may not write', () => {
    renderControl(makeBulk(false))
    expect(screen.queryByRole('button', { name: 'Set location' })).not.toBeInTheDocument()
  })
})
