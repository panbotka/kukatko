import { renderHook } from '@testing-library/react'
import { type ReactNode } from 'react'
import { I18nextProvider } from 'react-i18next'
import { beforeEach, describe, expect, it } from 'vitest'

import { ToastContext, type ToastOptions } from '../components/toast/ToastContext'
import i18n from '../i18n'

import { useUploadLimitToasts } from './useUploadLimitToasts'

/** The queue items the hook reads. */
type Items = { status: string; errorCode?: string }[]

/** Renders the hook over `items` with a toast recorder. */
function setup(initial: Items) {
  const toasts: ToastOptions[] = []
  const wrapper = ({ children }: { children: ReactNode }) => (
    <I18nextProvider i18n={i18n}>
      <ToastContext.Provider
        value={{
          show: (options) => {
            toasts.push(options)
          },
        }}
      >
        {children}
      </ToastContext.Provider>
    </I18nextProvider>
  )
  const hook = renderHook(
    ({ items }: { items: Items }) => {
      useUploadLimitToasts(items)
    },
    {
      wrapper,
      initialProps: { items: initial },
    },
  )
  return {
    toasts,
    rerender: (items: Items) => {
      hook.rerender({ items })
    },
  }
}

beforeEach(async () => {
  await i18n.changeLanguage('en')
})

describe('useUploadLimitToasts', () => {
  it('toasts each limit kind once, however many files it refuses', () => {
    const { toasts, rerender } = setup([{ status: 'error', errorCode: 'quota_exceeded' }])
    rerender([
      { status: 'error', errorCode: 'quota_exceeded' },
      { status: 'error', errorCode: 'quota_exceeded' },
      { status: 'error', errorCode: 'file_too_large' },
    ])
    expect(toasts).toEqual([
      {
        message: 'The project has reached its storage limit; no more files will be uploaded.',
        variant: 'danger',
      },
      {
        message: 'Some files are larger than the project allows and were not uploaded.',
        variant: 'danger',
      },
    ])
  })

  it('stays quiet for other failures and successes', () => {
    const { toasts } = setup([
      { status: 'error', errorCode: 'not_media' },
      { status: 'error' },
      { status: 'created' },
    ])
    expect(toasts).toEqual([])
  })

  it('toasts again for a new batch once the old refusals are gone', () => {
    const { toasts, rerender } = setup([{ status: 'error', errorCode: 'file_too_large' }])
    rerender([])
    rerender([{ status: 'error', errorCode: 'file_too_large' }])
    expect(toasts).toHaveLength(2)
  })
})
