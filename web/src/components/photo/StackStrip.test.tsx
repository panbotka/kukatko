import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { I18nextProvider } from 'react-i18next'
import { MemoryRouter } from 'react-router-dom'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import i18n from '../../i18n'
import type { StackMember } from '../../services/photos'

import { StackStrip } from './StackStrip'

/** Builds a stack member with the given overrides. */
function member(overrides: Partial<StackMember> = {}): StackMember {
  return {
    uid: 'ph1',
    file_name: 'IMG.jpg',
    media_type: 'image',
    file_mime: 'image/jpeg',
    file_width: 4000,
    file_height: 3000,
    file_size: 1_000_000,
    is_primary: false,
    thumb_url: '/thumb.jpg',
    ...overrides,
  }
}

interface Handlers {
  canWrite?: boolean
  onSetPrimary?: (uid: string) => Promise<void>
  onUnstackMember?: (uid: string) => Promise<void>
  onUnstackAll?: () => Promise<void>
}

function renderStrip(members: StackMember[], h: Handlers = {}) {
  const noop = () => Promise.resolve()
  return render(
    <I18nextProvider i18n={i18n}>
      <MemoryRouter>
        <StackStrip
          members={members}
          currentUid={members[0]?.uid ?? ''}
          canWrite={h.canWrite ?? true}
          onSetPrimary={h.onSetPrimary ?? noop}
          onUnstackMember={h.onUnstackMember ?? noop}
          onUnstackAll={h.onUnstackAll ?? noop}
        />
      </MemoryRouter>
    </I18nextProvider>,
  )
}

beforeEach(async () => {
  await i18n.changeLanguage('en')
})

describe('StackStrip', () => {
  it('renders nothing for fewer than two members', () => {
    const { container } = renderStrip([member({ uid: 'a', is_primary: true })])
    expect(container).toBeEmptyDOMElement()
  })

  it('lists every member and marks the primary', () => {
    renderStrip([
      member({ uid: 'a', file_name: 'IMG.jpg', is_primary: true }),
      member({ uid: 'b', file_name: 'IMG.CR2' }),
    ])
    expect(screen.getByText('IMG.jpg')).toBeInTheDocument()
    expect(screen.getByText('IMG.CR2')).toBeInTheDocument()
    expect(screen.getByText('Primary')).toBeInTheDocument()
  })

  it('lets an editor set a non-primary member as primary', async () => {
    const onSetPrimary = vi.fn().mockResolvedValue(undefined)
    renderStrip(
      [member({ uid: 'a', is_primary: true }), member({ uid: 'b', file_name: 'IMG.CR2' })],
      { onSetPrimary },
    )
    fireEvent.click(screen.getByRole('button', { name: 'Set as primary' }))
    await waitFor(() => {
      expect(onSetPrimary).toHaveBeenCalledWith('b')
    })
  })

  it('lets an editor unstack the whole stack', async () => {
    const onUnstackAll = vi.fn().mockResolvedValue(undefined)
    renderStrip([member({ uid: 'a', is_primary: true }), member({ uid: 'b' })], { onUnstackAll })
    fireEvent.click(screen.getByRole('button', { name: 'Unstack all' }))
    await waitFor(() => {
      expect(onUnstackAll).toHaveBeenCalled()
    })
  })

  it('hides all actions from a viewer', () => {
    renderStrip([member({ uid: 'a', is_primary: true }), member({ uid: 'b' })], { canWrite: false })
    expect(screen.queryByRole('button', { name: 'Set as primary' })).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Unstack' })).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Unstack all' })).not.toBeInTheDocument()
  })
})
