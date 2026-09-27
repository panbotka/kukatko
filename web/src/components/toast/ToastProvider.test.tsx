import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { I18nextProvider } from 'react-i18next'
import { beforeAll, describe, expect, it } from 'vitest'

import i18n from '../../i18n'

import { useToast } from './ToastContext'
import { ToastProvider } from './ToastProvider'

beforeAll(async () => {
  await i18n.changeLanguage('en')
})

/** A consumer that raises one toast on a click. */
function Raiser() {
  const toast = useToast()
  return (
    <button
      type="button"
      onClick={() => {
        toast.show({ message: 'Saved', variant: 'success' })
      }}
    >
      raise
    </button>
  )
}

function renderProvider() {
  return render(
    <I18nextProvider i18n={i18n}>
      <ToastProvider>
        <Raiser />
      </ToastProvider>
    </I18nextProvider>,
  )
}

/** The stack element the provider renders. */
function stack(container: HTMLElement): HTMLElement {
  const el = container.querySelector<HTMLElement>('.kk-toast-stack')
  if (el === null) {
    throw new Error('toast stack not rendered')
  }
  return el
}

describe('ToastProvider', () => {
  it('anchors the stack to the viewport, not the document', () => {
    const { container } = renderProvider()
    const el = stack(container)
    // Bootstrap's `.toast-container` is `position: absolute`, which resolves
    // against the document and paints the toast above the fold on a scrolled
    // page. `position-fixed` (a `!important` utility) is what overrides it, so
    // it has to reach the DOM.
    expect(el).toHaveClass('toast-container', 'position-fixed')
    expect(el).not.toHaveClass('position-absolute')
    expect(el).toHaveClass('top-0', 'start-50', 'translate-middle-x')
  })

  it('carries no p-* utility that would flatten the safe-area padding', () => {
    const { container } = renderProvider()
    // `.p-3` is `padding: 1rem !important` — it would beat the inset-aware
    // padding `.kk-toast-stack` declares.
    expect([...stack(container).classList].filter((c) => /^p[xytblrse]?-\d$/.test(c))).toEqual([])
  })

  it('shows a raised toast inside the stack and dismisses it on close', async () => {
    const user = userEvent.setup()
    const { container } = renderProvider()
    await user.click(screen.getByRole('button', { name: 'raise' }))
    expect(stack(container)).toHaveTextContent('Saved')
    await user.click(screen.getByRole('button', { name: i18n.t('toast.close') }))
    expect(stack(container)).not.toHaveTextContent('Saved')
  })
})
