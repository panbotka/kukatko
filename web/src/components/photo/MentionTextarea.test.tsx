import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { useState } from 'react'
import { I18nextProvider } from 'react-i18next'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import i18n from '../../i18n'

import { MentionTextarea } from './MentionTextarea'

vi.mock('../../services/directory', () => ({
  listDirectory: vi.fn(),
}))

const { listDirectory } = await import('../../services/directory')
const listDirectoryMock = vi.mocked(listDirectory)

const PEOPLE = [
  { uid: 'us1', name: 'Anna Nováková' },
  { uid: 'us2', name: 'Petr Kolář' },
  { uid: 'us3', name: 'Jana Kolářová' },
]

/** A stateful host, the way the composer owns its draft. */
function Host({ onKeyDown }: { onKeyDown?: (key: string) => void }) {
  const [value, setValue] = useState('')
  return (
    <I18nextProvider i18n={i18n}>
      <label htmlFor="box">Comment</label>
      <MentionTextarea
        id="box"
        value={value}
        onChange={setValue}
        onKeyDown={(event) => {
          onKeyDown?.(event.key)
        }}
      />
    </I18nextProvider>
  )
}

beforeEach(async () => {
  await i18n.changeLanguage('en')
  vi.clearAllMocks()
  listDirectoryMock.mockResolvedValue(PEOPLE)
})

describe('MentionTextarea', () => {
  it('fetches the directory only once @ is typed and filters it accent-insensitively', async () => {
    const user = userEvent.setup()
    render(<Host />)
    const box = screen.getByLabelText('Comment')

    await user.type(box, 'ahoj ')
    expect(listDirectoryMock).not.toHaveBeenCalled()
    expect(screen.queryByRole('listbox')).not.toBeInTheDocument()

    await user.type(box, '@kol')
    expect(listDirectoryMock).toHaveBeenCalledTimes(1)
    const options = await screen.findAllByRole('option')
    expect(options.map((o) => o.textContent)).toEqual(['Petr Kolář', 'Jana Kolářová'])
    expect(box).toHaveAttribute('aria-expanded', 'true')
  })

  it('picks a name on mousedown and inserts the token with the caret after it', async () => {
    const user = userEvent.setup()
    render(<Host />)
    const box = screen.getByLabelText<HTMLTextAreaElement>('Comment')

    await user.type(box, 'Hele @pet')
    const option = await screen.findByRole('option', { name: 'Petr Kolář' })
    // mousedown, not click: the row picks before the box can lose focus (and a
    // `.click()` alone must pick nothing, which is what the next line proves).
    fireEvent.click(option)
    expect(box).toHaveValue('Hele @pet')
    fireEvent.mouseDown(option)

    expect(box).toHaveValue('Hele @[Petr Kolář](user:us2) ')
    await waitFor(() => {
      expect(box.selectionStart).toBe('Hele @[Petr Kolář](user:us2) '.length)
    })
    expect(screen.queryByRole('listbox')).not.toBeInTheDocument()
  })

  it('moves with the arrows, picks with Enter without posting, and closes on Escape', async () => {
    const user = userEvent.setup()
    const onKeyDown = vi.fn()
    render(<Host onKeyDown={onKeyDown} />)
    const box = screen.getByLabelText('Comment')

    await user.type(box, '@kol')
    await screen.findAllByRole('option')
    await user.keyboard('{ArrowDown}')
    expect(screen.getByRole('option', { name: 'Jana Kolářová' })).toHaveAttribute(
      'aria-selected',
      'true',
    )
    await user.keyboard('{ArrowUp}')
    expect(screen.getByRole('option', { name: 'Petr Kolář' })).toHaveAttribute(
      'aria-selected',
      'true',
    )
    onKeyDown.mockClear()
    await user.keyboard('{Enter}')
    expect(box).toHaveValue('@[Petr Kolář](user:us2) ')
    // The Enter that picked a name never reached the composer, so nothing was posted.
    expect(onKeyDown).not.toHaveBeenCalledWith('Enter')

    await user.type(box, '@ja')
    await screen.findByRole('option', { name: 'Jana Kolářová' })
    await user.keyboard('{Escape}')
    expect(screen.queryByRole('listbox')).not.toBeInTheDocument()
    expect(box).toHaveValue('@[Petr Kolář](user:us2) @ja')
    // Typing on reopens it.
    await user.type(box, 'n')
    expect(await screen.findByRole('option', { name: 'Jana Kolářová' })).toBeInTheDocument()
  })

  it('hands other keys to the composer and says when nobody matches', async () => {
    const user = userEvent.setup()
    const onKeyDown = vi.fn()
    render(<Host onKeyDown={onKeyDown} />)
    const box = screen.getByLabelText('Comment')

    await user.type(box, 'x')
    expect(onKeyDown).toHaveBeenCalledWith('x')
    await user.type(box, ' @zzz')
    expect(await screen.findByText('Nobody by that name here')).toBeInTheDocument()
    // An Enter with nothing to pick is the composer's.
    onKeyDown.mockClear()
    await user.keyboard('{Enter}')
    expect(onKeyDown).toHaveBeenCalledWith('Enter')
  })

  it('offers nobody when the directory cannot be read, and the text stays text', async () => {
    listDirectoryMock.mockRejectedValue(new Error('down'))
    const user = userEvent.setup()
    render(<Host />)
    const box = screen.getByLabelText('Comment')

    await user.type(box, '@an')
    expect(await screen.findByText('Nobody by that name here')).toBeInTheDocument()
    expect(box).toHaveValue('@an')
  })
})
