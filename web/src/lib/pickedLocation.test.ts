import { beforeEach, describe, expect, it, vi } from 'vitest'

import { lastPickedLocation, rememberPickedLocation } from './pickedLocation'

beforeEach(() => {
  window.sessionStorage.clear()
})

describe('lastPickedLocation', () => {
  it('has nothing to offer before anything is picked', () => {
    expect(lastPickedLocation()).toBeNull()
  })

  it('hands back the coordinate picked last', () => {
    rememberPickedLocation({ lat: 49.19522, lng: 16.60796 })
    rememberPickedLocation({ lat: 48.95363, lng: 17.37649 })
    expect(lastPickedLocation()).toEqual({ lat: 48.95363, lng: 17.37649 })
  })

  it('refuses a stored value that is not a coordinate', () => {
    for (const stored of ['null', '"Brno"', '{"lat":"49"}', '{"lat":49}', 'not json']) {
      window.sessionStorage.setItem('kukatko.lastPickedLocation', stored)
      expect(lastPickedLocation()).toBeNull()
    }
  })

  it('refuses a coordinate outside the geographic range', () => {
    window.sessionStorage.setItem('kukatko.lastPickedLocation', '{"lat":491,"lng":16}')
    expect(lastPickedLocation()).toBeNull()
  })

  it('survives storage being unavailable', () => {
    // Spied on the prototype: jsdom's storage object forwards through a proxy, so
    // a spy installed on the instance never runs.
    vi.spyOn(Storage.prototype, 'setItem').mockImplementation(() => {
      throw new Error('quota exceeded')
    })
    vi.spyOn(Storage.prototype, 'getItem').mockImplementation(() => {
      throw new Error('storage disabled')
    })
    expect(() => {
      rememberPickedLocation({ lat: 49.19522, lng: 16.60796 })
    }).not.toThrow()
    expect(lastPickedLocation()).toBeNull()
  })
})
