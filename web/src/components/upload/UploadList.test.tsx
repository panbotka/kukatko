import { render, screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { type ReactNode } from 'react'
import { I18nextProvider } from 'react-i18next'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { type QueueItemStatus, type UploadQueueItem } from '../../hooks/useUploadQueue'
import i18n from '../../i18n'

import { UploadList } from './UploadList'

// jsdom has no layout, so the real virtualized list measures every row as zero
// tall and mounts none of them. This stand-in keeps the component under test —
// `UploadList` still decides what a row is — while rendering all of the rows,
// which is what the assertions here are about. The virtualization itself is a
// browser concern (see docs/FRONTEND.md) and is not what these tests cover.
vi.mock('react-virtuoso', () => ({
  Virtuoso: ({
    data,
    itemContent,
  }: {
    data: UploadQueueItem[]
    itemContent: (index: number, item: UploadQueueItem) => ReactNode
  }) => (
    <div data-testid="upload-list">
      {data.map((item, index) => (
        <div key={item.id}>{itemContent(index, item)}</div>
      ))}
    </div>
  ),
}))

/** A queue item over a file of the given name/type, in the given state. */
function item(
  id: string,
  name: string,
  type: string,
  status: QueueItemStatus = 'queued',
  extra: Partial<UploadQueueItem> = {},
): UploadQueueItem {
  return {
    id,
    file: new File(['data'], name, { type }),
    status,
    progress: 0,
    ...extra,
  }
}

function renderList(items: UploadQueueItem[]) {
  const onRemove = vi.fn()
  const onRetry = vi.fn()
  const view = render(
    <I18nextProvider i18n={i18n}>
      <UploadList items={items} onRemove={onRemove} onRetry={onRetry} />
    </I18nextProvider>,
  )
  return { ...view, onRemove, onRetry }
}

beforeEach(async () => {
  await i18n.changeLanguage('en')
})

describe('UploadList', () => {
  it('renders a row per queued file with its name and state', () => {
    renderList([item('u1', 'a.jpg', 'image/jpeg'), item('u2', 'b.jpg', 'image/jpeg', 'created')])

    expect(screen.getByText('a.jpg')).toBeInTheDocument()
    expect(screen.getByText('b.jpg')).toBeInTheDocument()
    expect(screen.getByText('Queued')).toBeInTheDocument()
    expect(screen.getByText('Uploaded')).toBeInTheDocument()
  })

  it('previews a picked photo from the browser copy, with no upload', () => {
    renderList([item('u1', 'a.jpg', 'image/jpeg')])

    const preview = within(screen.getByTestId('upload-thumb')).getByRole('presentation', {
      hidden: true,
    })
    expect(preview).toHaveAttribute('src', expect.stringMatching(/^blob:/))
  })

  it('draws a placeholder instead of a broken image for a video or a RAW file', () => {
    renderList([
      item('u1', 'clip.mov', 'video/quicktime'),
      item('u2', 'IMG_0042.HEIC', ''),
      item('u3', 'DSC_1000.nef', ''),
    ])

    // Nothing an `<img>` could paint: three placeholder frames, no image at all.
    expect(screen.getAllByTestId('upload-thumb')).toHaveLength(3)
    expect(screen.queryAllByRole('presentation', { hidden: true })).toHaveLength(0)
  })

  it('revokes an object URL when its row leaves the queue', () => {
    const revoke = vi.spyOn(URL, 'revokeObjectURL')
    const { rerender } = renderList([
      item('u1', 'a.jpg', 'image/jpeg'),
      item('u2', 'b.jpg', 'image/jpeg'),
    ])
    const urls = screen
      .getAllByRole('presentation', { hidden: true })
      .map((image) => image.getAttribute('src'))
    expect(urls).toHaveLength(2)
    expect(revoke).not.toHaveBeenCalled()

    // Clearing the queue (or scrolling a row out of a virtualized list) unmounts
    // the row, and the browser must get the file handle back.
    rerender(
      <I18nextProvider i18n={i18n}>
        <UploadList items={[]} onRemove={vi.fn()} onRetry={vi.fn()} />
      </I18nextProvider>,
    )
    expect(revoke.mock.calls.map(([url]) => url).sort()).toEqual([...urls].sort())
  })

  it('tints the preview frame with the row state', () => {
    renderList([
      item('u1', 'a.jpg', 'image/jpeg', 'uploading', { progress: 0.42 }),
      item('u2', 'b.jpg', 'image/jpeg', 'error', { error: 'boom' }),
    ])

    const [uploading, failed] = screen.getAllByTestId('upload-thumb')
    expect(uploading).toHaveClass('kk-upload-thumb--uploading')
    expect(within(uploading).getByText('42%')).toBeInTheDocument()
    expect(failed).toHaveClass('kk-upload-thumb--error')
  })

  it('keeps the per-row remove and retry actions', async () => {
    const user = userEvent.setup()
    const { onRemove, onRetry } = renderList([
      item('u1', 'a.jpg', 'image/jpeg', 'error', { error: 'boom' }),
    ])

    await user.click(screen.getByRole('button', { name: 'Retry' }))
    expect(onRetry).toHaveBeenCalledWith('u1')

    await user.click(screen.getByRole('button', { name: 'Remove' }))
    expect(onRemove).toHaveBeenCalledWith('u1')
  })
})
