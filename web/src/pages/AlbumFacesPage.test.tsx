import { render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { I18nextProvider } from 'react-i18next'
import { MemoryRouter, Route, Routes } from 'react-router-dom'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import i18n from '../i18n'
import { type Bbox, type FaceView, type FacesResponse, type Suggestion } from '../services/people'
import { type Photo, type PhotoListResponse } from '../services/photos'

import { AlbumFacesPage } from './AlbumFacesPage'

vi.mock('../services/people', async (importOriginal) => {
  const actual = await importOriginal<typeof import('../services/people')>()
  return { ...actual, fetchFaces: vi.fn(), assignFace: vi.fn() }
})
vi.mock('../services/photos', async (importOriginal) => {
  const actual = await importOriginal<typeof import('../services/photos')>()
  return { ...actual, fetchPhotos: vi.fn() }
})

const { fetchFaces, assignFace } = await import('../services/people')
const { fetchPhotos } = await import('../services/photos')
const facesMock = vi.mocked(fetchFaces)
const assignMock = vi.mocked(assignFace)
const photosMock = vi.mocked(fetchPhotos)

/** A catalogue row, only as far as the page reads it. */
function photo(uid: string): Photo {
  return {
    uid,
    file_hash: `h_${uid}`,
    file_name: `${uid}.jpg`,
    file_size: 1000,
    file_mime: 'image/jpeg',
    file_width: 1200,
    file_height: 800,
    taken_at_source: 'exif',
    media_type: 'image',
    created_at: '2026-01-01T00:00:00Z',
    updated_at: '2026-01-01T00:00:00Z',
  } as Photo
}

/** A ranked candidate at the given confidence. */
function suggestion(name: string, confidence: number): Suggestion {
  return { subject_uid: `su_${name}`, subject_name: name, distance: 1 - confidence, confidence }
}

/** An unnamed detection carrying the given suggestions. */
function face(faceIndex: number, suggestions: Suggestion[] = []): FaceView {
  return {
    face_index: faceIndex,
    bbox: [0.1, 0.1 * (faceIndex + 1), 0.2, 0.2] as Bbox,
    det_score: 0.9,
    action: 'create_marker',
    suggestions,
  }
}

/** Answers the queue search with one page holding exactly these photos. */
function queueOf(...uids: string[]): void {
  photosMock.mockImplementation((params) =>
    Promise.resolve({
      photos: (params.offset ?? 0) === 0 ? uids.map(photo) : [],
      total: uids.length,
      limit: params.limit ?? 200,
      offset: params.offset ?? 0,
      next_offset: null,
    } satisfies PhotoListResponse),
  )
}

/** Answers `GET /photos/{uid}/faces` from a table of photos. */
function facesFrom(table: Record<string, FaceView[]>): void {
  facesMock.mockImplementation((uid) => {
    // A photo missing from the table is one whose faces cannot be fetched.
    if (!Object.hasOwn(table, uid)) {
      return Promise.reject(new Error(`no faces for ${uid}`))
    }
    return Promise.resolve({
      photo_uid: uid,
      width: 1200,
      height: 800,
      orientation: 1,
      faces: table[uid],
    } satisfies FacesResponse)
  })
}

/** Mounts the page at an album's tagging route. */
function renderPage() {
  return render(
    <I18nextProvider i18n={i18n}>
      <MemoryRouter initialEntries={['/albums/al1/faces']}>
        <Routes>
          <Route path="/albums/:uid/faces" element={<AlbumFacesPage />} />
          <Route path="/albums/:uid" element={<div>album page</div>} />
        </Routes>
      </MemoryRouter>
    </I18nextProvider>,
  )
}

/** The list item asking about the face carrying `number`. */
function rowOf(number: number): HTMLElement {
  const badge = screen.getByText(String(number), { selector: '.badge' })
  const row = badge.closest('.kk-album-faces__row')
  if (row === null) {
    throw new Error(`row ${String(number)} not rendered`)
  }
  return row as HTMLElement
}

/** The scrolling block holding the rows, whose height the run freezes. */
function rowsBlock(): HTMLElement {
  const block = document.querySelector('.kk-album-faces__rows')
  if (block === null) {
    throw new Error('row list not rendered')
  }
  return block as HTMLElement
}

/**
 * Lays the row list out as 60 px per row and everything else as nothing, which is
 * the only layout jsdom can be made to have. It is what lets the frozen height be
 * read as a number of rows: a block still reporting three rows' worth after one
 * of them was answered is the whole point of the freeze.
 */
function measureRowsAt60px(): void {
  vi.spyOn(HTMLElement.prototype, 'getBoundingClientRect').mockImplementation(function (
    this: HTMLElement,
  ) {
    const rows = this.classList.contains('kk-album-faces__list')
      ? this.querySelectorAll('.kk-album-faces__row').length
      : 0
    return {
      width: 0,
      height: rows * 60,
      top: 0,
      left: 0,
      bottom: 0,
      right: 0,
      x: 0,
      y: 0,
      toJSON: () => ({}),
    }
  })
}

beforeEach(async () => {
  vi.clearAllMocks()
  assignMock.mockResolvedValue(undefined)
  await i18n.changeLanguage('cs')
})

describe('AlbumFacesPage', () => {
  it('shows the progress, the photo and one row per unnamed face', async () => {
    queueOf('p1', 'p2')
    facesFrom({
      p1: [face(0, [suggestion('Alice', 0.9)]), face(1, [suggestion('Bob', 0.42)])],
      p2: [face(0, [suggestion('Cyril', 0.8)])],
    })

    renderPage()

    expect(await screen.findByText('Fotka 1/2')).toBeInTheDocument()
    expect(screen.getByText('Alice')).toBeInTheDocument()
    // 0.42 clears the lowered display floor, so it is an offer like any other.
    expect(screen.getByText('Bob')).toBeInTheDocument()
    expect(screen.getByText(/42%/)).toBeInTheDocument()
  })

  it('confirms one face from its row and drops the row', async () => {
    const user = userEvent.setup()
    queueOf('p1')
    facesFrom({ p1: [face(0, [suggestion('Alice', 0.9)]), face(1, [suggestion('Bob', 0.8)])] })

    renderPage()
    await screen.findByText('Alice')
    await user.click(within(rowOf(1)).getByRole('button', { name: 'Potvrdit Alice' }))

    await waitFor(() => {
      expect(assignMock).toHaveBeenCalledTimes(1)
    })
    expect(screen.queryByText('Alice')).not.toBeInTheDocument()
    expect(screen.getByText('Bob')).toBeInTheDocument()
  })

  it('confirms every offer at once and says how many that is', async () => {
    const user = userEvent.setup()
    queueOf('p1', 'p2')
    facesFrom({
      p1: [face(0, [suggestion('Alice', 0.9)]), face(1, [suggestion('Bob', 0.8)])],
      p2: [face(0, [suggestion('Cyril', 0.8)])],
    })

    renderPage()
    const all = await screen.findByRole('button', { name: /Potvrdit vše \(2\)/ })
    await user.click(all)

    await waitFor(() => {
      expect(assignMock).toHaveBeenCalledTimes(2)
    })
    // Nothing left to ask on p1, so the run walks on by itself.
    await waitFor(() => {
      expect(screen.getByText('Fotka 2/2')).toBeInTheDocument()
    })
  })

  it('dismisses a row without writing anything', async () => {
    const user = userEvent.setup()
    queueOf('p1')
    facesFrom({ p1: [face(0, [suggestion('Alice', 0.9)]), face(1, [suggestion('Bob', 0.8)])] })

    renderPage()
    await screen.findByText('Alice')
    await user.click(within(rowOf(1)).getByRole('button', { name: 'Přeskočit obličej 1' }))

    expect(assignMock).not.toHaveBeenCalled()
    expect(screen.queryByText('Alice')).not.toBeInTheDocument()
    expect(screen.getByText('Bob')).toBeInTheDocument()
  })

  it('lists a face with no offered suggestion but gives it no controls', async () => {
    queueOf('p1')
    facesFrom({ p1: [face(0, [suggestion('Alice', 0.9)]), face(1, [suggestion('Bob', 0.2)])] })

    renderPage()
    await screen.findByText('Alice')

    const weak = rowOf(2)
    expect(within(weak).getByText(/Bez návrhu/)).toBeInTheDocument()
    expect(within(weak).queryAllByRole('button')).toHaveLength(0)
  })

  it('confirms the n-th face from the keyboard and advances with the arrow', async () => {
    const user = userEvent.setup()
    queueOf('p1', 'p2')
    facesFrom({
      p1: [face(0, [suggestion('Alice', 0.9)]), face(1, [suggestion('Bob', 0.8)])],
      p2: [face(0, [suggestion('Cyril', 0.8)])],
    })

    renderPage()
    await screen.findByText('Alice')
    await user.keyboard('2')

    await waitFor(() => {
      expect(assignMock).toHaveBeenCalledWith(
        'p1',
        expect.objectContaining({ subject_uid: 'su_Bob' }),
      )
    })

    await user.keyboard('{ArrowRight}')
    await waitFor(() => {
      expect(screen.getByText('Fotka 2/2')).toBeInTheDocument()
    })
  })

  it('goes back to the previous photo', async () => {
    const user = userEvent.setup()
    queueOf('p1', 'p2')
    facesFrom({
      p1: [face(0, [suggestion('Alice', 0.9)])],
      p2: [face(0, [suggestion('Cyril', 0.8)])],
    })

    renderPage()
    await screen.findByText('Alice')
    await user.keyboard('{ArrowRight}')
    await screen.findByText('Fotka 2/2')

    await user.keyboard('{ArrowLeft}')
    await waitFor(() => {
      expect(screen.getByText('Fotka 1/2')).toBeInTheDocument()
    })
  })

  it('reports a refused confirmation and keeps going', async () => {
    const user = userEvent.setup()
    queueOf('p1')
    facesFrom({ p1: [face(0, [suggestion('Alice', 0.9)]), face(1, [suggestion('Bob', 0.8)])] })
    assignMock.mockRejectedValue(new Error('nope'))

    renderPage()
    await screen.findByText('Alice')
    await user.click(within(rowOf(1)).getByRole('button', { name: 'Potvrdit Alice' }))

    expect(await screen.findByText(/nepodařilo potvrdit/)).toBeInTheDocument()
    expect(screen.getByText('Alice')).toBeInTheDocument()
  })

  it('says the album is done when nothing in it can be answered', async () => {
    queueOf('p1')
    facesFrom({ p1: [face(0, [suggestion('Alice', 0.2)])] })

    renderPage()

    expect(await screen.findByText(/tohle album je projité/)).toBeInTheDocument()
  })

  it('holds the row list at the height it opened with', async () => {
    const user = userEvent.setup()
    measureRowsAt60px()
    queueOf('p1')
    facesFrom({
      p1: [
        face(0, [suggestion('Alice', 0.9)]),
        face(1, [suggestion('Bob', 0.8)]),
        face(2, [suggestion('Cyril', 0.8)]),
      ],
    })

    renderPage()
    await screen.findByText('Alice')
    const block = rowsBlock()
    expect(block).toHaveStyle({ height: '180px' })

    await user.click(within(rowOf(1)).getByRole('button', { name: 'Potvrdit Alice' }))
    await waitFor(() => {
      expect(screen.queryByText('Alice')).not.toBeInTheDocument()
    })

    // Two rows left, so the list is now 120 px of content — and the block it sits
    // in is expected not to have noticed, because the photograph above it is
    // sized against the height left over.
    expect(block).toHaveStyle({ height: '180px' })
  })

  it("reserves the next photo's own height when the run moves on", async () => {
    const user = userEvent.setup()
    measureRowsAt60px()
    queueOf('p1', 'p2')
    facesFrom({
      p1: [
        face(0, [suggestion('Alice', 0.9)]),
        face(1, [suggestion('Bob', 0.8)]),
        face(2, [suggestion('Cyril', 0.8)]),
      ],
      p2: [face(0, [suggestion('Dana', 0.8)])],
    })

    renderPage()
    await screen.findByText('Alice')
    expect(rowsBlock()).toHaveStyle({ height: '180px' })

    await user.keyboard('{ArrowRight}')
    await screen.findByText('Dana')

    expect(rowsBlock()).toHaveStyle({ height: '60px' })
  })

  it('leaves the remaining rows on their own numbers after a confirmation', async () => {
    const user = userEvent.setup()
    queueOf('p1')
    facesFrom({
      p1: [
        face(0, [suggestion('Alice', 0.9)]),
        face(1, [suggestion('Bob', 0.8)]),
        face(2, [suggestion('Cyril', 0.8)]),
      ],
    })

    renderPage()
    await screen.findByText('Alice')
    await user.click(within(rowOf(1)).getByRole('button', { name: 'Potvrdit Alice' }))
    await waitFor(() => {
      expect(screen.queryByText('Alice')).not.toBeInTheDocument()
    })

    // Renumbering would put Bob on 1 — and 1 is still drawn on Alice's box.
    expect(screen.queryByText('1', { selector: '.badge' })).not.toBeInTheDocument()
    expect(within(rowOf(2)).getByText('Bob')).toBeInTheDocument()
    expect(within(rowOf(3)).getByText('Cyril')).toBeInTheDocument()

    // And the digit still reaches the face it is drawn on.
    await user.keyboard('3')
    await waitFor(() => {
      expect(assignMock).toHaveBeenCalledWith(
        'p1',
        expect.objectContaining({ subject_uid: 'su_Cyril' }),
      )
    })
  })

  it('closes back to the album', async () => {
    const user = userEvent.setup()
    queueOf('p1')
    facesFrom({ p1: [face(0, [suggestion('Alice', 0.9)])] })

    renderPage()
    await screen.findByText('Alice')
    await user.click(screen.getByRole('button', { name: /Zavřít/ }))

    expect(await screen.findByText('album page')).toBeInTheDocument()
  })
})
