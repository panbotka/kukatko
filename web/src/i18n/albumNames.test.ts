import { describe, expect, it } from 'vitest'

import { albumDisplayTitle } from './albumNames'

describe('albumDisplayTitle', () => {
  it('renders an English month album in Czech', () => {
    expect(albumDisplayTitle('January 2026', 'cs')).toBe('leden 2026')
    expect(albumDisplayTitle('May 2026', 'cs')).toBe('květen 2026')
    expect(albumDisplayTitle('December 1998', 'cs')).toBe('prosinec 1998')
  })

  it('renders a bare month name too', () => {
    expect(albumDisplayTitle('September', 'cs')).toBe('září')
  })

  it('accepts a region subtag, as a negotiated language carries one', () => {
    expect(albumDisplayTitle('January 2026', 'cs-CZ')).toBe('leden 2026')
  })

  it('renders a known country name in Czech, in both its English forms', () => {
    expect(albumDisplayTitle('Czechia', 'cs')).toBe('Česko')
    expect(albumDisplayTitle('Czech Republic', 'cs')).toBe('Česko')
    expect(albumDisplayTitle('United Kingdom', 'cs')).toBe('Spojené království')
  })

  it('renders a place album composed of a country and a year in Czech', () => {
    // The importers wrote hundreds of these next to hand-named Czech albums.
    expect(albumDisplayTitle('Czech Republic 2026', 'cs')).toBe('Česko 2026')
    expect(albumDisplayTitle('Czech Republic 2025', 'cs')).toBe('Česko 2025')
    expect(albumDisplayTitle('Austria 2019', 'cs')).toBe('Rakousko 2019')
  })

  it('leaves everything else exactly as it is stored', () => {
    // The whole point: a hand-written album name is data, not a pattern to fix.
    expect(albumDisplayTitle('Dovolená 2019', 'cs')).toBe('Dovolená 2019')
    expect(albumDisplayTitle('Pets', 'cs')).toBe('Pets')
    expect(albumDisplayTitle('Leden 2026', 'cs')).toBe('Leden 2026')
  })

  it('rewrites only an exact match, never a name that merely starts with a month', () => {
    expect(albumDisplayTitle('January in Norway', 'cs')).toBe('January in Norway')
    expect(albumDisplayTitle('May Day', 'cs')).toBe('May Day')
    expect(albumDisplayTitle('March 26th', 'cs')).toBe('March 26th')
    expect(albumDisplayTitle('New Zealand trip', 'cs')).toBe('New Zealand trip')
  })

  it('leaves the stored English name alone on an English UI', () => {
    expect(albumDisplayTitle('January 2026', 'en')).toBe('January 2026')
    expect(albumDisplayTitle('Czechia', 'en')).toBe('Czechia')
  })

  it('survives a blank or padded title', () => {
    expect(albumDisplayTitle('   ', 'cs')).toBe('   ')
    expect(albumDisplayTitle('  January 2026  ', 'cs')).toBe('leden 2026')
  })
})
