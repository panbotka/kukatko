import { beforeEach, describe, expect, it, vi } from 'vitest'

import {
  DEFAULT_PLAYBACK_RATE,
  PLAYBACK_RATES,
  formatPlaybackTime,
  isPlaybackRate,
  playbackFraction,
  positionFromPointer,
  previewOffset,
  readPlaybackRate,
  seekTarget,
  stepPlaybackRate,
  storyboardTileIndex,
  storyboardTileStyle,
  writePlaybackRate,
  type StoryboardSpec,
} from './videoPlayback'

/** A two-row sprite of 160×90 tiles, one every two seconds. */
const spec: StoryboardSpec = {
  columns: 10,
  rows: 2,
  count: 20,
  tile_width: 160,
  tile_height: 90,
  interval_ms: 2000,
}

beforeEach(() => {
  window.sessionStorage.clear()
})

describe('PLAYBACK_RATES', () => {
  it('offers the five documented speeds, slowest first, including normal', () => {
    expect([...PLAYBACK_RATES]).toEqual([0.5, 1, 1.25, 1.5, 2])
    expect(PLAYBACK_RATES).toContain(DEFAULT_PLAYBACK_RATE)
  })
})

describe('isPlaybackRate', () => {
  it('accepts only the offered speeds', () => {
    expect(isPlaybackRate(1.25)).toBe(true)
    expect(isPlaybackRate(3)).toBe(false)
    expect(isPlaybackRate('1.5')).toBe(false)
    expect(isPlaybackRate(null)).toBe(false)
  })
})

describe('readPlaybackRate / writePlaybackRate', () => {
  it('round-trips a chosen rate through session storage', () => {
    writePlaybackRate(1.5)
    expect(readPlaybackRate()).toBe(1.5)
  })

  it('falls back to normal speed when nothing is remembered', () => {
    expect(readPlaybackRate()).toBe(DEFAULT_PLAYBACK_RATE)
  })

  it('ignores a stored value the player does not offer', () => {
    window.sessionStorage.setItem('kukatko.video.rate', '7')
    expect(readPlaybackRate()).toBe(DEFAULT_PLAYBACK_RATE)
    window.sessionStorage.setItem('kukatko.video.rate', 'fast')
    expect(readPlaybackRate()).toBe(DEFAULT_PLAYBACK_RATE)
  })

  it('survives storage being unavailable', () => {
    const getItem = vi.spyOn(Storage.prototype, 'getItem').mockImplementation(() => {
      throw new Error('denied')
    })
    const setItem = vi.spyOn(Storage.prototype, 'setItem').mockImplementation(() => {
      throw new Error('denied')
    })
    expect(readPlaybackRate()).toBe(DEFAULT_PLAYBACK_RATE)
    expect(() => {
      writePlaybackRate(2)
    }).not.toThrow()
    getItem.mockRestore()
    setItem.mockRestore()
  })
})

describe('stepPlaybackRate', () => {
  it('moves one step through the list and saturates at both ends', () => {
    expect(stepPlaybackRate(1, 1)).toBe(1.25)
    expect(stepPlaybackRate(1, -1)).toBe(0.5)
    expect(stepPlaybackRate(0.5, -1)).toBe(0.5)
    expect(stepPlaybackRate(2, 1)).toBe(2)
  })

  it('treats an unknown current rate as normal speed', () => {
    expect(stepPlaybackRate(3, 1)).toBe(1.25)
    expect(stepPlaybackRate(Number.NaN, -1)).toBe(0.5)
  })
})

describe('seekTarget', () => {
  it('clamps a skip inside the clip', () => {
    expect(seekTarget(30, 10, 60)).toBe(40)
    expect(seekTarget(4, -10, 60)).toBe(0)
    expect(seekTarget(55, 10, 60)).toBe(60)
  })

  it('only clamps below zero while the duration is unknown', () => {
    expect(seekTarget(5, 10, Number.NaN)).toBe(15)
    expect(seekTarget(5, -10, 0)).toBe(0)
  })

  it('treats a non-finite position as the start', () => {
    expect(seekTarget(Number.NaN, 10, 60)).toBe(10)
  })
})

describe('formatPlaybackTime', () => {
  it('reads as m:ss below an hour and h:mm:ss above it', () => {
    expect(formatPlaybackTime(0)).toBe('0:00')
    expect(formatPlaybackTime(9)).toBe('0:09')
    expect(formatPlaybackTime(75)).toBe('1:15')
    expect(formatPlaybackTime(3661)).toBe('1:01:01')
  })

  it('never renders NaN for an unloaded or negative position', () => {
    expect(formatPlaybackTime(Number.NaN)).toBe('0:00')
    expect(formatPlaybackTime(-5)).toBe('0:00')
    expect(formatPlaybackTime(Number.POSITIVE_INFINITY)).toBe('0:00')
  })
})

describe('playbackFraction', () => {
  it('reports the played share as a 0..1 fraction', () => {
    expect(playbackFraction(30, 60)).toBe(0.5)
    expect(playbackFraction(0, 60)).toBe(0)
    expect(playbackFraction(90, 60)).toBe(1)
  })

  it('reads as zero while the duration is unknown', () => {
    expect(playbackFraction(30, 0)).toBe(0)
    expect(playbackFraction(30, Number.NaN)).toBe(0)
  })
})

describe('storyboardTileIndex', () => {
  it('maps a position to the tile covering it, clamped into the grid', () => {
    expect(storyboardTileIndex(spec, 0)).toBe(0)
    expect(storyboardTileIndex(spec, 1.9)).toBe(0)
    expect(storyboardTileIndex(spec, 2)).toBe(1)
    expect(storyboardTileIndex(spec, 39)).toBe(19)
    expect(storyboardTileIndex(spec, 10000)).toBe(19)
    expect(storyboardTileIndex(spec, -1)).toBe(0)
  })

  it('answers zero for a degenerate grid rather than dividing by zero', () => {
    expect(storyboardTileIndex({ ...spec, interval_ms: 0 }, 5)).toBe(0)
    expect(storyboardTileIndex({ ...spec, count: 0 }, 5)).toBe(0)
    expect(storyboardTileIndex(spec, Number.NaN)).toBe(0)
  })
})

describe('storyboardTileStyle', () => {
  it('sizes the box to one tile and offsets the sprite to it', () => {
    // Tile 11 is row 1, column 1 in a ten-wide grid.
    const style = storyboardTileStyle(spec, '/sprite.jpg', 11)
    expect(style).toEqual({
      width: '160px',
      height: '90px',
      backgroundImage: 'url(/sprite.jpg)',
      backgroundSize: '1600px 180px',
      backgroundPosition: '-160px -90px',
    })
  })

  it('places the first tile at the sprite origin', () => {
    expect(storyboardTileStyle(spec, '/sprite.jpg', 0).backgroundPosition).toBe('-0px -0px')
  })
})

describe('previewOffset', () => {
  it('follows the cursor but keeps the preview inside the track', () => {
    expect(previewOffset(400, 800, 160)).toBe(400)
    expect(previewOffset(10, 800, 160)).toBe(80)
    expect(previewOffset(790, 800, 160)).toBe(720)
  })

  it('centres the preview when the track is narrower than it', () => {
    expect(previewOffset(30, 100, 160)).toBe(50)
  })
})

describe('positionFromPointer', () => {
  it('maps an x within the track to a playback position', () => {
    expect(positionFromPointer(0, 200, 60)).toBe(0)
    expect(positionFromPointer(100, 200, 60)).toBe(30)
    expect(positionFromPointer(200, 200, 60)).toBe(60)
  })

  it('clamps a drag that left the track to its ends', () => {
    expect(positionFromPointer(-50, 200, 60)).toBe(0)
    expect(positionFromPointer(500, 200, 60)).toBe(60)
  })

  it('answers zero when there is no track or no duration yet', () => {
    expect(positionFromPointer(50, 0, 60)).toBe(0)
    expect(positionFromPointer(50, 200, 0)).toBe(0)
  })
})
