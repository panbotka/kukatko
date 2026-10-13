import { describe, expect, it } from 'vitest'

import i18n from '../i18n'

import { formatLimits, limitDraft, parseLimit } from './byteLimit'

const MB = 1024 * 1024
const GB = 1024 * MB

describe('limitDraft', () => {
  it('edits no limit as an empty field', () => {
    expect(limitDraft(null)).toEqual({ amount: '', unit: 'MB' })
  })

  it('edits whole gigabytes in GB and anything else in MB', () => {
    expect(limitDraft(2 * GB)).toEqual({ amount: '2', unit: 'GB' })
    expect(limitDraft(50 * MB)).toEqual({ amount: '50', unit: 'MB' })
    expect(limitDraft(1.5 * GB)).toEqual({ amount: '1536', unit: 'MB' })
    expect(limitDraft(1234567)).toEqual({ amount: '1.18', unit: 'MB' })
  })
})

describe('parseLimit', () => {
  it('reads an empty field as no limit', () => {
    expect(parseLimit({ amount: '  ', unit: 'GB' })).toEqual({ ok: true, bytes: null })
  })

  it('reads an amount in its unit, with a decimal comma or point', () => {
    expect(parseLimit({ amount: '2', unit: 'GB' })).toEqual({ ok: true, bytes: 2 * GB })
    expect(parseLimit({ amount: '1,5', unit: 'MB' })).toEqual({ ok: true, bytes: 1.5 * MB })
    expect(parseLimit({ amount: '0.5', unit: 'GB' })).toEqual({ ok: true, bytes: GB / 2 })
  })

  it('refuses anything that is not a positive number', () => {
    for (const amount of ['0', '-1', 'abc', '1e3', '1.2.3']) {
      expect(parseLimit({ amount, unit: 'MB' })).toEqual({ ok: false })
    }
  })

  it('round-trips a stored limit', () => {
    for (const bytes of [GB, 3 * GB, 50 * MB, 700 * MB]) {
      expect(parseLimit(limitDraft(bytes))).toEqual({ ok: true, bytes })
    }
  })
})

describe('formatLimits', () => {
  const t = i18n.getFixedT('cs')

  it('says "neomezeno" once when neither limit is set', () => {
    expect(formatLimits({ max_file_bytes: null, max_total_bytes: null }, t, 'cs')).toBe('neomezeno')
  })

  it('names each limit in readable bytes', () => {
    expect(formatLimits({ max_file_bytes: 50 * MB, max_total_bytes: null }, t, 'cs')).toBe(
      'soubor 50,0 MB · celkem neomezeno',
    )
  })
})
