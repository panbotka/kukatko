import { render, screen } from '@testing-library/react'
import { describe, expect, it } from 'vitest'

import { entityBadgeClassName, entityIcon, type EntityKind } from './entity'
import { EntityBadge } from './EntityBadge'

const KINDS: EntityKind[] = ['album', 'label', 'person']

describe('EntityBadge colour convention', () => {
  it('gives album, tag and person three distinct colour classes', () => {
    const classes = KINDS.map(entityBadgeClassName)
    expect(new Set(classes).size).toBe(3)
    expect(classes).toEqual(['kk-entity-album', 'kk-entity-label', 'kk-entity-person'])
  })

  it('marks each kind with its own leading icon', () => {
    expect(entityIcon('album')).toBe('collection')
    expect(entityIcon('label')).toBe('tags')
    expect(entityIcon('person')).toBe('person-circle')
  })

  it('renders a badge carrying the kind colour class, a decorative icon and the label', () => {
    render(<EntityBadge kind="album">Holidays</EntityBadge>)

    const badge = screen.getByText('Holidays')
    expect(badge).toHaveClass('badge', 'kk-entity-album')

    const icon = badge.querySelector('i.bi-collection')
    expect(icon).not.toBeNull()
    // Colour is only an aid; the glyph is decorative and the label carries meaning.
    expect(icon).toHaveAttribute('aria-hidden', 'true')
  })

  it('appends extra classes after the kind colour class', () => {
    render(
      <EntityBadge kind="label" className="fw-normal">
        beach
      </EntityBadge>,
    )

    const badge = screen.getByText('beach')
    expect(badge).toHaveClass('kk-entity-label', 'fw-normal')
  })
})
