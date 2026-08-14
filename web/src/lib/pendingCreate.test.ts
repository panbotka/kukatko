import { describe, expect, it, vi } from 'vitest'

import {
  hasPending,
  pendingName,
  pendingOptions,
  pendingValue,
  resolvePending,
} from './pendingCreate'

describe('pending markers', () => {
  it('round-trips a name through a marker and leaves real UIDs alone', () => {
    expect(pendingName(pendingValue('Ostatky 2022'))).toBe('Ostatky 2022')
    // Real UIDs are short base32 strings that never carry the prefix.
    expect(pendingName('al7k3m')).toBeNull()
  })

  it('labels a pending pick by its name so the chip never shows the marker', () => {
    expect(pendingOptions([pendingValue('Ostatky 2022'), 'al7k3m'])).toEqual([
      { value: 'create:Ostatky 2022', label: 'Ostatky 2022' },
    ])
  })

  it('reports whether anything still has to be created', () => {
    expect(hasPending(['al7k3m', pendingValue('Nové')])).toBe(true)
    expect(hasPending(['al7k3m'])).toBe(false)
    expect(hasPending([])).toBe(false)
  })
})

describe('resolvePending', () => {
  it('creates every pending name and swaps its fresh UID in, in place', async () => {
    const create = vi.fn((name: string) => Promise.resolve(`uid-${name}`))

    const outcome = await resolvePending(
      [pendingValue('Ostatky'), 'al7k3m', pendingValue('Pouť')],
      create,
    )

    expect(outcome).toEqual({ status: 'resolved', values: ['uid-Ostatky', 'al7k3m', 'uid-Pouť'] })
    expect(create).toHaveBeenCalledTimes(2)
    expect(create.mock.calls.map(([name]) => name)).toEqual(['Ostatky', 'Pouť'])
  })

  it('never asks the server anything when nothing is pending', async () => {
    const create = vi.fn(() => Promise.resolve('unused'))

    expect(await resolvePending(['al7k3m', 'al9x2p'], create)).toEqual({
      status: 'resolved',
      values: ['al7k3m', 'al9x2p'],
    })
    expect(create).not.toHaveBeenCalled()
  })

  it('stops at the first failure, naming it and keeping what it already created', async () => {
    const boom = new Error('title already used')
    const create = vi.fn((name: string) =>
      name === 'Pouť' ? Promise.reject(boom) : Promise.resolve(`uid-${name}`),
    )

    const outcome = await resolvePending(
      [pendingValue('Ostatky'), pendingValue('Pouť'), pendingValue('Hody')],
      create,
    )

    // The album created before the failure keeps its real UID, so retrying the
    // batch never makes a second album of that name; the rest is untouched.
    expect(outcome).toEqual({
      status: 'failed',
      values: ['uid-Ostatky', 'create:Pouť', 'create:Hody'],
      name: 'Pouť',
      error: boom,
    })
    expect(create).toHaveBeenCalledTimes(2)
  })
})
