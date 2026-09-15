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
  { uid: 'su1', label: 'Bohumil Nečas', alias: 'Bohouš', hint: '12' },
  { uid: 'su2', label: 'Marie Nečasová' },
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
    await user.click(screen.getByRole('option', { name: /Bohumil Nečas/ }))

    expect(onAdd).toHaveBeenCalledWith('su1')
    expect(screen.getByRole('combobox')).toHaveValue('')
  })

  it('picks the highlighted row with the keyboard', async () => {
    const user = userEvent.setup()
    const { onAdd } = renderField()

    await user.type(screen.getByRole('combobox'), 'nečas{ArrowDown}{ArrowDown}{Enter}')

    expect(onAdd).toHaveBeenCalledWith('su2')
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
