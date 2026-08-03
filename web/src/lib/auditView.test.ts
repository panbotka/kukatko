import { describe, expect, it } from 'vitest'

import { type AuditRecord } from '../services/audit'

import {
  AUDIT_DEFAULTS,
  AUDIT_DETAIL_LINK_LIMIT,
  auditDetailLinks,
  auditTargetHref,
  pickFilters,
  viewToParams,
} from './auditView'

/** A minimal audit record; every case overrides only what it is about. */
function record(overrides: Partial<AuditRecord> = {}): AuditRecord {
  return {
    id: 1,
    actor_uid: 'us1',
    action: 'photo.update',
    target_type: 'photos',
    target_uid: 'ph9',
    details: null,
    ip: null,
    user_agent: null,
    created_at: '2026-07-11T10:00:00Z',
    ...overrides,
  }
}

describe('viewToParams', () => {
  it('expands the date filters to RFC 3339 day boundaries in UTC', () => {
    const params = viewToParams({ ...AUDIT_DEFAULTS, since: '2026-07-01', until: '2026-07-31' })

    expect(params.since).toBe('2026-07-01T00:00:00Z')
    expect(params.until).toBe('2026-07-31T23:59:59Z')
    expect(params.offset).toBe(0)
  })

  it('omits an empty date and keeps the offset', () => {
    const params = viewToParams({ ...AUDIT_DEFAULTS, offset: '100' })

    expect(params.since).toBeUndefined()
    expect(params.until).toBeUndefined()
    expect(params.offset).toBe(100)
  })
})

describe('pickFilters', () => {
  it('drops the offset from the view', () => {
    expect(pickFilters({ ...AUDIT_DEFAULTS, action: 'photo.update', offset: '100' })).toEqual({
      user: '',
      action: 'photo.update',
      entity_type: '',
      entity_uid: '',
      since: '',
      until: '',
    })
  })
})

describe('auditTargetHref', () => {
  it.each([
    ['photos', 'ph9', '/photos/ph9'],
    ['albums', 'al7', '/albums/al7'],
    ['labels', 'lb5', '/labels/lb5'],
    ['subjects', 'su3', '/people/su3'],
  ])('routes a %s target to its detail page', (targetType, uid, href) => {
    expect(auditTargetHref(record({ target_type: targetType, target_uid: uid }))).toBe(href)
  })

  it.each(['users', 'api_tokens', 'announcement', 'audit_log'])(
    'has nowhere to send a %s target',
    (targetType) => {
      expect(auditTargetHref(record({ target_type: targetType, target_uid: 'x1' }))).toBeNull()
    },
  )

  it('routes a marker through the photo its details name', () => {
    const href = auditTargetHref(
      record({
        action: 'face.unassign',
        target_type: 'markers',
        target_uid: 'mt8rdjbfhgma1zfg',
        details: { action: 'unassign_person', photo_uid: 'phakulc72calcifi6immisvfl4' },
      }),
    )

    expect(href).toBe('/photos/phakulc72calcifi6immisvfl4')
  })

  it('lands on the person’s marker when the details name a subject', () => {
    const href = auditTargetHref(
      record({
        action: 'face.assign',
        target_type: 'markers',
        target_uid: 'mk3rq71npuo1h9a4burkdqhjmi',
        details: { photo_uid: 'ph8qpckcoo1vnesecdaqs3f5pv', subject_uid: 'suht2a' },
      }),
    )

    expect(href).toBe('/photos/ph8qpckcoo1vnesecdaqs3f5pv?person=suht2a&info=1')
  })

  it('has nowhere to send a marker whose details name no photo', () => {
    expect(
      auditTargetHref(record({ target_type: 'markers', target_uid: 'mk1', details: {} })),
    ).toBeNull()
  })

  it('has nowhere to send a bulk action, which names no target', () => {
    expect(
      auditTargetHref(
        record({ action: 'photos.bulk', target_uid: null, details: { photo_uids: ['ph1'] } }),
      ),
    ).toBeNull()
  })
})

describe('auditDetailLinks', () => {
  it('finds nothing in an empty payload', () => {
    expect(auditDetailLinks(null)).toEqual({ groups: [], hidden: 0 })
    expect(auditDetailLinks({ field: 'title', count: 3 })).toEqual({ groups: [], hidden: 0 })
  })

  it('links the photo a label rejection happened on', () => {
    const { groups } = auditDetailLinks({ via: 'review', photo_uid: 'ph9e3e2uluvsukb4h93b0vbpt9' })

    expect(groups).toEqual([
      {
        key: 'photo_uid',
        links: [{ uid: 'ph9e3e2uluvsukb4h93b0vbpt9', href: '/photos/ph9e3e2uluvsukb4h93b0vbpt9' }],
      },
    ])
  })

  it('links every photo of a bulk action, grouped under its key', () => {
    const { groups, hidden } = auditDetailLinks({ photo_uids: ['ph1', 'ph2'], count: 2 })

    expect(hidden).toBe(0)
    expect(groups).toEqual([
      {
        key: 'photo_uids',
        links: [
          { uid: 'ph1', href: '/photos/ph1' },
          { uid: 'ph2', href: '/photos/ph2' },
        ],
      },
    ])
  })

  it('maps each known entity key onto the route of its own detail page', () => {
    const { groups } = auditDetailLinks({
      album_uid: 'al7',
      label_uid: 'lb5',
      subject_uid: 'su3',
    })

    expect(groups.map((group) => [group.key, group.links[0].href])).toEqual([
      ['album_uid', '/albums/al7'],
      ['label_uid', '/labels/lb5'],
      ['subject_uid', '/people/su3'],
    ])
  })

  it('sends a marker to the photo of the same payload, focused on the person', () => {
    const { groups } = auditDetailLinks({
      photo_uid: 'ph1',
      subject_uid: 'su3',
      marker_uid: 'mk1',
    })

    expect(groups).toEqual([
      { key: 'photo_uid', links: [{ uid: 'ph1', href: '/photos/ph1' }] },
      { key: 'subject_uid', links: [{ uid: 'su3', href: '/people/su3' }] },
      { key: 'marker_uid', links: [{ uid: 'mk1', href: '/photos/ph1?person=su3&info=1' }] },
    ])
  })

  it('does not repeat a destination another key already reached', () => {
    // face.unassign names no subject, so the marker resolves to the bare photo —
    // the very link `photo_uid` already provides.
    const { groups } = auditDetailLinks({ photo_uid: 'ph1', marker_uid: 'mk1' })

    expect(groups).toEqual([{ key: 'photo_uid', links: [{ uid: 'ph1', href: '/photos/ph1' }] }])
  })

  it('ignores uid keys of things with no page and malformed values', () => {
    const { groups } = auditDetailLinks({
      user_uid: 'us1',
      ps_uid: 'ps1',
      photoprism_uid: 'pp1',
      photo_uid: '',
      album_uid: 42,
      label_uids: ['lb1', 7, ''],
    })

    expect(groups).toEqual([{ key: 'label_uids', links: [{ uid: 'lb1', href: '/labels/lb1' }] }])
  })

  it('stops at the limit and reports what it left to the raw payload', () => {
    const uids = Array.from({ length: AUDIT_DETAIL_LINK_LIMIT + 5 }, (_, i) => `ph${String(i)}`)

    const { groups, hidden } = auditDetailLinks({ photo_uids: uids })

    expect(groups[0].links).toHaveLength(AUDIT_DETAIL_LINK_LIMIT)
    expect(hidden).toBe(5)
  })
})
