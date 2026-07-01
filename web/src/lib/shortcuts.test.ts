import { afterEach, describe, expect, it } from 'vitest'

import csCommon from '../i18n/locales/cs/common.json'
import enCommon from '../i18n/locales/en/common.json'

import { isFormModalOpen, SHORTCUT_GROUPS, shortcutToken } from './shortcuts'

/** Resolves a dot-separated i18n key against a nested resource tree. */
function resolve(tree: unknown, key: string): unknown {
  return key.split('.').reduce<unknown>((node, part) => {
    if (node !== null && typeof node === 'object' && part in node) {
      return (node as Record<string, unknown>)[part]
    }
    return undefined
  }, tree)
}

afterEach(() => {
  document.body.innerHTML = ''
})

describe('shortcutToken', () => {
  it('lower-cases single-character keys so Shift variants match', () => {
    expect(shortcutToken('f')).toBe('f')
    expect(shortcutToken('F')).toBe('f')
  })

  it('keeps ? (Shift+/) as its own token', () => {
    expect(shortcutToken('?')).toBe('?')
  })

  it('passes named keys through unchanged', () => {
    expect(shortcutToken('ArrowUp')).toBe('ArrowUp')
    expect(shortcutToken('Enter')).toBe('Enter')
    expect(shortcutToken('Escape')).toBe('Escape')
  })
})

describe('isFormModalOpen', () => {
  it('is false when no modal is open', () => {
    expect(isFormModalOpen()).toBe(false)
  })

  it('is true for an open modal containing a form control', () => {
    document.body.innerHTML =
      '<div class="modal show"><div class="modal-body"><input /></div></div>'
    expect(isFormModalOpen()).toBe(true)
  })

  it('is false for an open modal with no form control (e.g. the help overlay)', () => {
    document.body.innerHTML =
      '<div class="modal show"><div class="modal-body"><button>Close</button></div></div>'
    expect(isFormModalOpen()).toBe(false)
  })

  it('ignores a hidden (not-shown) form modal', () => {
    document.body.innerHTML = '<div class="modal"><input /></div>'
    expect(isFormModalOpen()).toBe(false)
  })
})

describe('SHORTCUT_GROUPS', () => {
  it('every title and description key resolves to a string in both languages', () => {
    for (const group of SHORTCUT_GROUPS) {
      for (const tree of [csCommon, enCommon]) {
        expect(typeof resolve(tree, group.titleKey), group.titleKey).toBe('string')
        for (const entry of group.entries) {
          expect(typeof resolve(tree, entry.descriptionKey), entry.descriptionKey).toBe('string')
          expect(entry.keys.length).toBeGreaterThan(0)
        }
      }
    }
  })
})
