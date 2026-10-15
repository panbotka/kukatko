import { beforeEach, describe, expect, it, vi } from 'vitest'

import { lastPickedLocation, rememberPickedLocation } from './pickedLocation'

beforeEach(() => {
  window.sessionStorage.clear()
})

describe('lastPickedLocation', () => {
  it('has nothing to offer before anything is picked', () => {
    expect(lastPickedLocation('v')).toBeNull()
  })

  it('hands back the coordinate picked last', () => {
    rememberPickedLocation('v', { lat: 49.19522, lng: 16.60796 })
    rememberPickedLocation('v', { lat: 48.95363, lng: 17.37649 })
    expect(lastPickedLocation('v')).toEqual({ lat: 48.95363, lng: 17.37649 })
  })

  it("keeps each project's pick to itself", () => {
    rememberPickedLocation('v', { lat: 49.19522, lng: 16.60796 })
    expect(lastPickedLocation('rodina')).toBeNull()
    rememberPickedLocation('rodina', { lat: 50.08804, lng: 14.42076 })
    expect(lastPickedLocation('v')).toEqual({ lat: 49.19522, lng: 16.60796 })
    expect(lastPickedLocation('rodina')).toEqual({ lat: 50.08804, lng: 14.42076 })
  })

  it('remembers nothing and offers nothing without a project', () => {
    rememberPickedLocation(null, { lat: 49.19522, lng: 16.60796 })
    expect(window.sessionStorage.length).toBe(0)
    expect(lastPickedLocation(null)).toBeNull()
  })

  it('refuses a stored value that is not a coordinate', () => {
    for (const stored of ['null', '"Brno"', '{"lat":"49"}', '{"lat":49}', 'not json']) {
      window.sessionStorage.setItem('kukatko.lastPickedLocation.v', stored)
      expect(lastPickedLocation('v')).toBeNull()
    }
  })

  it('refuses a coordinate outside the geographic range', () => {
    window.sessionStorage.setItem('kukatko.lastPickedLocation.v', '{"lat":491,"lng":16}')
    expect(lastPickedLocation('v')).toBeNull()
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
      rememberPickedLocation('v', { lat: 49.19522, lng: 16.60796 })
    }).not.toThrow()
    expect(lastPickedLocation('v')).toBeNull()
  })
})
