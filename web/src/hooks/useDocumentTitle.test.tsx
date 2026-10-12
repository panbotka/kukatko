import { render } from '@testing-library/react'
import { I18nextProvider } from 'react-i18next'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import i18n from '../i18n'
import { ProjectContext, type ProjectContextValue } from '../project/ProjectContext'

import { useDocumentTitle } from './useDocumentTitle'

/** A page-sized stand-in: it does nothing but claim the tab title. */
function Page({ title }: { title: string | null | undefined }) {
  useDocumentTitle(title)
  return <p>page</p>
}

/** Mounts `Page` under the app's own i18next instance, as a route would. */
function renderPage(title: string | null | undefined) {
  return render(
    <I18nextProvider i18n={i18n}>
      <Page title={title} />
    </I18nextProvider>,
  )
}

beforeEach(async () => {
  await i18n.changeLanguage('cs')
  document.title = 'Kukátko'
})

afterEach(async () => {
  await i18n.changeLanguage('cs')
})

describe('useDocumentTitle', () => {
  it('names the tab after the page, with the app name behind a separator', () => {
    renderPage('Knihovna')

    expect(document.title).toBe('Knihovna · Kukátko')
  })

  it('follows the page name when it changes, e.g. once a photo loads', () => {
    const { rerender } = renderPage(null)

    // Nothing to say yet: the bare app name, never a stale or invented one.
    expect(document.title).toBe('Kukátko')

    rerender(
      <I18nextProvider i18n={i18n}>
        <Page title="Svatba 1965" />
      </I18nextProvider>,
    )

    expect(document.title).toBe('Svatba 1965 · Kukátko')
  })

  it.each([null, undefined, '', '   '])(
    'falls back to the bare app name for %p',
    (title: string | null | undefined) => {
      renderPage(title)

      expect(document.title).toBe('Kukátko')
    },
  )

  it('trims the page name rather than pushing the separator away from it', () => {
    renderPage('  Alba  ')

    expect(document.title).toBe('Alba · Kukátko')
  })

  it('resets the tab to the app name when the page unmounts', () => {
    const { unmount } = renderPage('Svatba 1965')
    expect(document.title).toBe('Svatba 1965 · Kukátko')

    unmount()

    // This is what keeps a photo's name off the library: an untitled page can
    // never inherit the previous one's title.
    expect(document.title).toBe('Kukátko')
  })

  it('leaves the new page in charge when one titled page replaces another', () => {
    // React runs the outgoing page's cleanup before the incoming page's effect,
    // so the reset above must not win over the page that navigated in.
    const { unmount } = renderPage('Svatba 1965')
    unmount()
    renderPage('Knihovna')

    expect(document.title).toBe('Knihovna · Kukátko')
  })

  it('re-titles the tab when the UI language changes under it', async () => {
    const { rerender } = renderPage('Knihovna')
    expect(document.title).toBe('Knihovna · Kukátko')

    await i18n.changeLanguage('en')
    rerender(
      <I18nextProvider i18n={i18n}>
        <Page title="Library" />
      </I18nextProvider>,
    )

    expect(document.title).toBe('Library · Kukátko')
  })

  describe('inside a project', () => {
    /** A provider value: the project „Veselice", named by the address or not. */
    function projectValue(inAddress: boolean): ProjectContextValue {
      const project = {
        uid: 'p1',
        slug: 'v',
        name: 'Veselice',
        active: true,
        role: 'viewer' as const,
        approved: true,
      }
      return { project, projects: [project], status: 'ready', inAddress, switchProject: vi.fn() }
    }

    it('names the project between the page and the app', () => {
      render(
        <I18nextProvider i18n={i18n}>
          <ProjectContext.Provider value={projectValue(true)}>
            <Page title="Alba" />
          </ProjectContext.Provider>
        </I18nextProvider>,
      )

      expect(document.title).toBe('Alba · Veselice · Kukátko')
    })

    it('leaves the project out on an instance page, which belongs to none', () => {
      // `/account`: the context still holds the entry project as a fallback.
      render(
        <I18nextProvider i18n={i18n}>
          <ProjectContext.Provider value={projectValue(false)}>
            <Page title="Účet" />
          </ProjectContext.Provider>
        </I18nextProvider>,
      )

      expect(document.title).toBe('Účet · Kukátko')
    })

    it('still falls back to the bare app name while the page has no name', () => {
      render(
        <I18nextProvider i18n={i18n}>
          <ProjectContext.Provider value={projectValue(true)}>
            <Page title={null} />
          </ProjectContext.Provider>
        </I18nextProvider>,
      )

      expect(document.title).toBe('Kukátko')
    })
  })
})
