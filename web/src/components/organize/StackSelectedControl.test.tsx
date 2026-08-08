import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { I18nextProvider } from 'react-i18next'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { AuthContext, type AuthContextValue } from '../../auth/AuthContext'
import { useBulkEdit } from '../../hooks/useBulkEdit'
import i18n from '../../i18n'
import { type PhotoDetail } from '../../services/photos'
import { expectLive, expectOff } from '../../test/reasoned'

import { StackSelectedControl } from './StackSelectedControl'

vi.mock('../../services/photos', async (importOriginal) => {
  const actual = await importOriginal<typeof import('../../services/photos')>()
  return { ...actual, stackPhotos: vi.fn() }
})

const { stackPhotos } = await import('../../services/photos')
const stackMock = vi.mocked(stackPhotos)

const onEdited = vi.fn()

/** The stack endpoint answers with the primary photo's refreshed detail. */
const stacked = { uid: 'ph1' } as PhotoDetail

function auth(canWrite: boolean): AuthContextValue {
  return {
    status: 'authenticated',
    user: { uid: 'u1', username: 'u', display_name: 'U', role: canWrite ? 'editor' : 'viewer' },
    role: canWrite ? 'editor' : 'viewer',
    downloadToken: null,
    canWrite,
    isAdmin: false,
    login: vi.fn(),
    logout: vi.fn(),
    refresh: vi.fn(),
  } as unknown as AuthContextValue
}

/** The smallest page that wires a selection to the control under test. */
function Harness() {
  const bulk = useBulkEdit({ onEdited })
  return (
    <>
      <button
        type="button"
        onClick={() => {
          bulk.selection.toggle('ph1')
        }}
      >
        toggle ph1
      </button>
      <button
        type="button"
        onClick={() => {
          bulk.selection.toggle('ph2')
        }}
      >
        toggle ph2
      </button>
      <StackSelectedControl bulk={bulk} />
    </>
  )
}

function renderControl(canWrite = true) {
  return render(
    <I18nextProvider i18n={i18n}>
      <AuthContext.Provider value={auth(canWrite)}>
        <Harness />
      </AuthContext.Provider>
    </I18nextProvider>,
  )
}

beforeEach(async () => {
  await i18n.changeLanguage('en')
  stackMock.mockReset()
  onEdited.mockReset()
})

describe('StackSelectedControl', () => {
  it('is hidden entirely from a viewer', () => {
    renderControl(false)

    expect(screen.queryByRole('button', { name: 'Stack selected' })).not.toBeInTheDocument()
  })

  it('asks for a second photo instead of going dead in silence', async () => {
    const user = userEvent.setup()
    renderControl()

    expectOff(
      screen.getByRole('button', { name: 'Stack selected' }),
      'A stack needs at least two photos — select another one.',
    )

    await user.click(screen.getByRole('button', { name: 'toggle ph1' }))
    expectOff(
      screen.getByRole('button', { name: 'Stack selected' }),
      'A stack needs at least two photos — select another one.',
    )

    await user.click(screen.getByRole('button', { name: 'toggle ph2' }))
    expectLive(screen.getByRole('button', { name: 'Stack selected' }))
  })

  it('says it is working, not that it is refusing, while the request runs', async () => {
    let settle: (photo: PhotoDetail) => void = () => undefined
    stackMock.mockReturnValue(
      new Promise<PhotoDetail>((resolve) => {
        settle = resolve
      }),
    )
    const user = userEvent.setup()
    renderControl()

    await user.click(screen.getByRole('button', { name: 'toggle ph1' }))
    await user.click(screen.getByRole('button', { name: 'toggle ph2' }))
    await user.click(screen.getByRole('button', { name: 'Stack selected' }))

    expect(stackMock).toHaveBeenCalledWith(['ph1', 'ph2'])
    // The busy reason, not the "pick another photo" one: the reader is told to
    // wait, never to do something they have already done.
    expectOff(
      screen.getByRole('button', { name: 'Stack selected' }),
      'The photos are being stacked — one moment.',
    )

    settle(stacked)
    await waitFor(() => {
      expect(onEdited).toHaveBeenCalled()
    })
  })

  it('does not fire a second request while it is off', async () => {
    const user = userEvent.setup()
    renderControl()

    await user.click(screen.getByRole('button', { name: 'toggle ph1' }))
    await user.click(screen.getByRole('button', { name: 'Stack selected' }))

    expect(stackMock).not.toHaveBeenCalled()
  })
})
