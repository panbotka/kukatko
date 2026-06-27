import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { I18nextProvider } from 'react-i18next'
import { MemoryRouter } from 'react-router-dom'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import i18n from '../../i18n'
import { type FacesResponse } from '../../services/people'

import { FaceOverlay } from './FaceOverlay'

// Mock only the network calls; keep the rest of the module (types) real.
vi.mock('../../services/people', async (importOriginal) => {
  const actual = await importOriginal<typeof import('../../services/people')>()
  return { ...actual, fetchFaces: vi.fn(), assignFace: vi.fn() }
})

const { fetchFaces, assignFace } = await import('../../services/people')
const fetchMock = vi.mocked(fetchFaces)
const assignMock = vi.mocked(assignFace)

/** A faces response with one unnamed face carrying one suggestion. */
function facesResponse(): FacesResponse {
  return {
    photo_uid: 'ph1',
    width: 1000,
    height: 800,
    orientation: 1,
    faces: [
      {
        face_index: 0,
        bbox: [0.1, 0.2, 0.3, 0.4],
        det_score: 0.9,
        action: 'create_marker',
        suggestions: [
          { subject_uid: 'su_a', subject_name: 'Alice', distance: 0.1, confidence: 0.9 },
        ],
      },
    ],
  }
}

function renderOverlay() {
  return render(
    <I18nextProvider i18n={i18n}>
      <MemoryRouter>
        <FaceOverlay photoUid="ph1" />
      </MemoryRouter>
    </I18nextProvider>,
  )
}

beforeEach(async () => {
  await i18n.changeLanguage('en')
  fetchMock.mockReset()
  assignMock.mockReset()
  assignMock.mockResolvedValue(undefined)
})

afterEach(() => {
  vi.restoreAllMocks()
})

describe('FaceOverlay', () => {
  it('positions each face box from the normalized bbox', async () => {
    fetchMock.mockResolvedValue(facesResponse())
    renderOverlay()

    const box = await screen.findByRole('button', { name: 'Unnamed face 1' })
    expect(box).toHaveStyle({ left: '10%', top: '20%', width: '30%', height: '40%' })
  })

  it('accepts a suggestion with one tap and calls the assign API', async () => {
    fetchMock.mockResolvedValue(facesResponse())
    const user = userEvent.setup()
    renderOverlay()

    await user.click(await screen.findByRole('button', { name: 'Unnamed face 1' }))
    await user.click(screen.getByRole('button', { name: /Alice/ }))

    await waitFor(() => {
      expect(assignMock).toHaveBeenCalledWith('ph1', {
        action: 'create_marker',
        bbox: [0.1, 0.2, 0.3, 0.4],
        face_index: 0,
        subject_uid: 'su_a',
      })
    })
  })

  it('assigns a free-text name from the panel', async () => {
    fetchMock.mockResolvedValue(facesResponse())
    const user = userEvent.setup()
    renderOverlay()

    await user.click(await screen.findByRole('button', { name: 'Unnamed face 1' }))
    await user.type(screen.getByLabelText('Name'), 'Bob')
    await user.click(screen.getByRole('button', { name: 'Assign' }))

    await waitFor(() => {
      expect(assignMock).toHaveBeenCalledWith('ph1', {
        action: 'create_marker',
        bbox: [0.1, 0.2, 0.3, 0.4],
        face_index: 0,
        subject_name: 'Bob',
      })
    })
  })
})
