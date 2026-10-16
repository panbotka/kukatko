import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { I18nextProvider } from 'react-i18next'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import i18n from '../../i18n'
import { readCss, zIndexOf } from '../../test/css'

import { AddAutocomplete, type AutocompleteOption } from './AddAutocomplete'

/**
 * Points `window.matchMedia` at a fixed phone/desktop answer, driving the
 * `useAnchoredMenu` hook the field uses to pick its suggestion-list layout.
 * The shared test setup stubs a non-matching (desktop) `matchMedia`.
 */
function mockViewport(narrow: boolean): void {
  window.matchMedia = vi.fn().mockImplementation((query: string) => ({
    matches: narrow,
    media: query,
    onchange: null,
    addEventListener: vi.fn(),
    removeEventListener: vi.fn(),
    addListener: vi.fn(),
    removeListener: vi.fn(),
    dispatchEvent: vi.fn(),
  }))
}

const OPTIONS: AutocompleteOption[] = [
  { uid: 'su1', label: 'Bohumil Nedvěd', alias: 'Bohouš', hint: '12' },
  { uid: 'su2', label: 'Marie Nedvědová' },
  { uid: 'su3', label: 'Petr Doležal' },
]

/** Renders the field with the given handlers inside the i18n provider. */
function renderField(props: Partial<Parameters<typeof AddAutocomplete>[0]> = {}) {
  const onAdd = vi.fn()
  render(
    <I18nextProvider i18n={i18n}>
      <AddAutocomplete
        id="person"
        label="Person’s name"
        options={OPTIONS}
        onAdd={onAdd}
        {...props}
      />
    </I18nextProvider>,
  )
  return { onAdd }
}

beforeEach(async () => {
  await i18n.changeLanguage('en')
})

afterEach(() => {
  // Restore the shared desktop default so a phone test never leaks into the next.
  mockViewport(false)
})

describe('AddAutocomplete', () => {
  it('offers what the query names, by label or by nickname, and hands back the uid', async () => {
    const user = userEvent.setup()
    const { onAdd } = renderField()

    // Accent- and case-insensitive, and the nickname searches like the name does.
    await user.type(screen.getByRole('combobox'), 'bohous')
    await user.click(screen.getByRole('option', { name: /Bohumil Nedvěd/ }))

    expect(onAdd).toHaveBeenCalledWith('su1')
    expect(screen.getByRole('combobox')).toHaveValue('')
  })

  it('picks the highlighted row with the keyboard', async () => {
    const user = userEvent.setup()
    const { onAdd } = renderField()

    await user.type(screen.getByRole('combobox'), 'nedvěd{ArrowDown}{Enter}')

    expect(onAdd).toHaveBeenCalledWith('su2')
  })

  it('makes the first row the active option, so Enter alone picks it', async () => {
    const user = userEvent.setup()
    const { onAdd } = renderField()
    const input = screen.getByRole('combobox')

    await user.type(input, 'nedvěd')
    const first = screen.getByRole('option', { name: /Bohumil Nedvěd/ })
    expect(input).toHaveAttribute('aria-activedescendant', first.id)
    expect(first).toHaveAttribute('aria-selected', 'true')

    await user.keyboard('{Enter}')
    expect(onAdd).toHaveBeenCalledWith('su1')
    expect(input).toHaveValue('')
  })

  it('picks a partial match with Enter rather than offering to create the typed text', async () => {
    // The pre-release repro: "Jedna" lists "Jedna jediná" above "Create «Jedna»".
    const user = userEvent.setup()
    const onCreate = vi.fn().mockResolvedValue(true)
    const { onAdd } = renderField({
      options: [{ uid: 'al1', label: 'Jedna jediná' }],
      onCreate,
    })

    await user.type(screen.getByRole('combobox'), 'Jedna{Enter}')

    expect(onAdd).toHaveBeenCalledWith('al1')
    expect(onCreate).not.toHaveBeenCalled()
  })

  it('ranks an exact name before the longer names that contain it', async () => {
    const user = userEvent.setup()
    const { onAdd } = renderField({
      options: [
        { uid: 'al1', label: 'Jedna jediná' },
        { uid: 'al2', label: 'Jedna' },
      ],
      onCreate: vi.fn(),
    })

    await user.type(screen.getByRole('combobox'), 'jedna')
    expect(screen.getAllByRole('option').map((option) => option.textContent)).toEqual([
      'Jedna',
      'Jedna jediná',
    ])

    await user.keyboard('{Enter}')
    expect(onAdd).toHaveBeenCalledWith('al2')
  })

  it('creates with Enter alone when the create row is all there is', async () => {
    const user = userEvent.setup()
    const onCreate = vi.fn().mockResolvedValue(true)
    const { onAdd } = renderField({ onCreate })
    const input = screen.getByRole('combobox')

    await user.type(input, 'Anežka')
    expect(input).toHaveAttribute(
      'aria-activedescendant',
      screen.getByRole('option', { name: 'Create “Anežka”' }).id,
    )
    await user.keyboard('{Enter}')

    expect(onCreate).toHaveBeenCalledWith('Anežka')
    expect(onAdd).not.toHaveBeenCalled()
  })

  it('lets Enter through untouched while no list is shown', async () => {
    const user = userEvent.setup()
    const { onAdd } = renderField()
    const input = screen.getByRole('combobox')

    await user.click(input)
    await user.keyboard('{Enter}')
    expect(input).not.toHaveAttribute('aria-activedescendant')

    await user.type(input, 'nedvěd{Escape}{Enter}')
    expect(onAdd).not.toHaveBeenCalled()
  })

  it('offers to create a name nothing carries and keeps the text when that fails', async () => {
    const user = userEvent.setup()
    const onCreate = vi.fn().mockResolvedValue(false)
    renderField({ onCreate })

    await user.type(screen.getByRole('combobox'), 'Anežka')
    await user.click(screen.getByRole('option', { name: 'Create “Anežka”' }))

    expect(onCreate).toHaveBeenCalledWith('Anežka')
    // The mutation failed, so the typed name survives for a retry.
    expect(screen.getByRole('combobox')).toHaveValue('Anežka')
  })

  it('renders the suggestions as a fixed overlay on desktop so a scrollable modal cannot clip them', async () => {
    mockViewport(false)
    const user = userEvent.setup()
    renderField()

    await user.type(screen.getByRole('combobox'), 'doležal')

    // A fixed overlay is measured off the viewport, not clipped by any ancestor
    // `overflow: auto` — `AddRelationModal`'s scrollable body left 28px of a
    // 426px list visible before this. Every row is reachable instead.
    const listbox = screen.getByRole('listbox', { name: 'Person’s name' })
    expect(listbox).toHaveStyle({ position: 'fixed' })
    expect(listbox).toHaveClass('kk-overlay-menu')
    expect(screen.getByRole('option', { name: /Petr Doležal/ })).toBeInTheDocument()
  })

  it('paints the desktop overlay above a page that has its own sticky toolbar', async () => {
    mockViewport(false)
    const user = userEvent.setup()
    render(
      <I18nextProvider i18n={i18n}>
        {/* A page's own in-page sticky bar, which Bootstrap's `.dropdown-menu`
            z-index of 1000 loses to. */}
        <div className="kukatko-sticky-toolbar">Uploading…</div>
        <AddAutocomplete id="person" label="Person’s name" options={OPTIONS} onAdd={vi.fn()} />
      </I18nextProvider>,
    )

    await user.type(screen.getByRole('combobox'), 'doležal')

    // jsdom loads no stylesheet, so what the class is worth is read out of the
    // shipped one: it has to win over the toolbar rendered above.
    const css = readCss('src/styles/app.css')
    expect(zIndexOf(css, /\.kk-overlay-menu\s*(?=\{)/)).toBeGreaterThan(
      zIndexOf(css, /\.kukatko-sticky-toolbar\s*(?=\{)/),
    )
  })

  it('flows the suggestions inside the scroll on a phone so they clear the keyboard', async () => {
    mockViewport(true)
    const user = userEvent.setup()
    renderField()

    await user.type(screen.getByRole('combobox'), 'doležal')

    // In the dialog's own scroll flow (static), never a fixed box that a soft
    // keyboard would cover, and the match is still offered.
    const listbox = screen.getByRole('listbox', { name: 'Person’s name' })
    expect(listbox).toHaveClass('position-static')
    expect(listbox).not.toHaveStyle({ position: 'fixed' })
    expect(screen.getByRole('option', { name: /Petr Doležal/ })).toBeInTheDocument()
  })
})

/**
 * The field never takes the keyboard by itself: only a `focusKey` asks for it,
 * and a request made while the field is disabled waits for it to be enabled —
 * the face panel asks during the mutation that moved it to the next face.
 */
describe('AddAutocomplete and the keyboard', () => {
  function Field({ focusKey, disabled }: { focusKey?: number | null; disabled?: boolean }) {
    return (
      <I18nextProvider i18n={i18n}>
        <AddAutocomplete
          id="person"
          label="Person’s name"
          options={OPTIONS}
          onAdd={vi.fn()}
          focusKey={focusKey}
          disabled={disabled}
        />
      </I18nextProvider>
    )
  }

  it('leaves the focus alone without a focusKey', () => {
    render(<Field />)
    expect(screen.getByRole('combobox')).not.toHaveFocus()
  })

  it('takes the focus for a focusKey, and again when it changes', () => {
    const { rerender } = render(<Field focusKey={1} />)
    const input = screen.getByRole('combobox')
    expect(input).toHaveFocus()

    input.blur()
    expect(input).not.toHaveFocus()
    rerender(<Field focusKey={2} />)
    expect(input).toHaveFocus()
  })

  it('waits for the field to be enabled before taking the focus', () => {
    const { rerender } = render(<Field focusKey={1} disabled />)
    const input = screen.getByRole('combobox')
    expect(input).not.toHaveFocus()

    rerender(<Field focusKey={1} disabled={false} />)
    expect(input).toHaveFocus()
  })
})
