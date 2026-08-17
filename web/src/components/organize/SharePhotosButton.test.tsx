import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { I18nextProvider } from 'react-i18next'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import i18n from '../../i18n'
import { type ShareManifestFile } from '../../lib/photoShare'
import { ApiError } from '../../services/auth'

import { SharePhotosButton, type SharePhotosButtonProps } from './SharePhotosButton'

vi.mock('../../services/share', () => ({
  fetchShareManifest: vi.fn(),
  fetchShareFile: vi.fn(),
}))

const { fetchShareManifest, fetchShareFile } = await import('../../services/share')
const manifestMock = vi.mocked(fetchShareManifest)
const fileMock = vi.mocked(fetchShareFile)

/** A manifest entry named after its index. */
function entry(index: number): ShareManifestFile {
  return { uid: `p${index}`, name: `p${index}.jpg`, mime: 'image/jpeg', size: 1024, preview: false }
}

let share: ReturnType<typeof vi.fn>

/** Makes this browser one that can hand files to a share sheet. */
function stubShareSheet() {
  share = vi.fn().mockResolvedValue(undefined)
  Object.defineProperty(navigator, 'share', { value: share, configurable: true })
  Object.defineProperty(navigator, 'canShare', { value: () => true, configurable: true })
}

function renderButton(props: Partial<SharePhotosButtonProps> = {}) {
  return render(
    <I18nextProvider i18n={i18n}>
      <SharePhotosButton photoUids={['p1']} {...props} />
    </I18nextProvider>,
  )
}

beforeEach(async () => {
  await i18n.changeLanguage('en')
  stubShareSheet()
  manifestMock.mockResolvedValue([entry(1)])
  fileMock.mockImplementation((file) =>
    Promise.resolve(new File([new Uint8Array([1])], file.name, { type: file.mime })),
  )
})

afterEach(() => {
  Reflect.deleteProperty(navigator, 'share')
  Reflect.deleteProperty(navigator, 'canShare')
})

describe('SharePhotosButton', () => {
  it('renders nothing where the browser cannot share files', () => {
    Reflect.deleteProperty(navigator, 'canShare')

    renderButton()

    expect(screen.queryByRole('button')).not.toBeInTheDocument()
  })

  it('hands the selection to the share sheet on a tap', async () => {
    const user = userEvent.setup()
    renderButton({ photoUids: ['p1', 'p2'] })

    await user.click(screen.getByRole('button', { name: 'Share' }))

    await waitFor(() => {
      expect(share).toHaveBeenCalledTimes(1)
    })
    expect(manifestMock).toHaveBeenCalledWith(['p1', 'p2'], expect.anything())
  })

  it('is disabled with an empty selection', () => {
    renderButton({ photoUids: [] })

    expect(screen.getByRole('button')).toBeDisabled()
  })

  it('counts the files up while a batch is being prepared', async () => {
    manifestMock.mockResolvedValue([entry(1), entry(2)])
    let release!: (file: File) => void
    fileMock.mockImplementationOnce((file) =>
      Promise.resolve(new File([new Uint8Array([1])], file.name, { type: file.mime })),
    )
    fileMock.mockImplementationOnce(
      () =>
        new Promise<File>((resolve) => {
          release = resolve
        }),
    )
    const user = userEvent.setup()
    renderButton({ photoUids: ['p1', 'p2'] })

    await user.click(screen.getByRole('button'))

    await waitFor(() => {
      expect(screen.getByRole('button')).toHaveTextContent('Preparing 1 of 2')
    })
    expect(screen.getByRole('button')).toBeDisabled()

    release(new File([new Uint8Array([2])], 'p2.jpg', { type: 'image/jpeg' }))
    await waitFor(() => {
      expect(share).toHaveBeenCalled()
    })
  })

  it('asks for a tap per batch and says how far the sequence has got', async () => {
    manifestMock.mockResolvedValue(Array.from({ length: 25 }, (_v, i) => entry(i)))
    const user = userEvent.setup()
    renderButton()

    await user.click(screen.getByRole('button'))

    const next = await screen.findByRole('button', { name: 'Share batch 2 of 2' })
    expect(screen.getByText('Shared 1 of 2 batches')).toBeInTheDocument()

    await user.click(next)

    await waitFor(() => {
      expect(share).toHaveBeenCalledTimes(2)
    })
    await waitFor(() => {
      expect(screen.getByRole('button', { name: 'Share' })).toBeEnabled()
    })
  })

  it('says nothing when the reader closes the share sheet', async () => {
    share.mockRejectedValueOnce(new DOMException('cancelled', 'AbortError'))
    const user = userEvent.setup()
    renderButton()

    await user.click(screen.getByRole('button'))

    await waitFor(() => {
      expect(screen.getByRole('button', { name: 'Share' })).toBeEnabled()
    })
    expect(screen.queryByRole('alert')).not.toBeInTheDocument()
  })

  it('names the photo it could not load', async () => {
    manifestMock.mockResolvedValue([entry(1), entry(2)])
    fileMock.mockRejectedValueOnce(new ApiError(404, 'original file not found'))
    const user = userEvent.setup()
    renderButton()

    await user.click(screen.getByRole('button'))

    await waitFor(() => {
      expect(screen.getByRole('alert')).toHaveTextContent('p1.jpg could not be loaded.')
    })
    // The rest of the batch was still handed over.
    expect(share).toHaveBeenCalledTimes(1)
  })

  it('explains an over-cap selection', async () => {
    manifestMock.mockRejectedValue(new ApiError(413, 'too many photos'))
    const user = userEvent.setup()
    renderButton()

    await user.click(screen.getByRole('button'))

    await waitFor(() => {
      expect(screen.getByRole('alert')).toHaveTextContent(/Too many photos for one share/)
    })
    expect(share).not.toHaveBeenCalled()
  })

  it('speaks Czech by default', async () => {
    await i18n.changeLanguage('cs')
    manifestMock.mockResolvedValue(Array.from({ length: 25 }, (_v, i) => entry(i)))
    const user = userEvent.setup()
    renderButton()

    expect(screen.getByRole('button', { name: 'Sdílet' })).toBeInTheDocument()

    await user.click(screen.getByRole('button'))

    expect(await screen.findByRole('button', { name: 'Sdílet dávku 2 z 2' })).toBeInTheDocument()
    expect(screen.getByText('Sdíleno 1 z 2 dávek')).toBeInTheDocument()
  })
})
