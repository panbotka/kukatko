import remarkParse from 'remark-parse'
import { unified } from 'unified'
import { describe, expect, it } from 'vitest'

import {
  commentAnchorId,
  filterMentionCandidates,
  findMentionQuery,
  insertMention,
  MAX_MENTION_SUGGESTIONS,
  mentionSanitizeSchema,
  mentionToken,
  remarkMentions,
} from './mentions'

/** Parses body with the plugin and returns the tree as JSON-friendly data. */
function parse(body: string): unknown {
  const processor = unified().use(remarkParse).use(remarkMentions)
  const tree = processor.parse(body)
  processor.runSync(tree)
  return JSON.parse(
    JSON.stringify(tree, (key, value: unknown) => (key === 'position' ? undefined : value)),
  )
}

/** Every node of a given type in the parsed tree, depth first. */
function nodesOf(tree: unknown, type: string): Record<string, unknown>[] {
  const out: Record<string, unknown>[] = []
  const walk = (node: unknown) => {
    if (typeof node !== 'object' || node === null) {
      return
    }
    const record = node as Record<string, unknown>
    if (record.type === type) {
      out.push(record)
    }
    if (Array.isArray(record.children)) {
      record.children.forEach(walk)
    }
  }
  walk(tree)
  return out
}

describe('mentionToken', () => {
  it('writes the token the backend recognises, brackets and line breaks dropped', () => {
    expect(mentionToken('Petr Kolář', 'us1')).toBe('@[Petr Kolář](user:us1)')
    expect(mentionToken(' An[na] N\nk ', 'us1')).toBe('@[Anna N k](user:us1)')
  })
})

describe('remarkMentions', () => {
  it('turns @[name](user:uid) into a mention node and keeps the text around it', () => {
    const tree = parse('Hele @[Anna Nová](user:us1), víš to?')
    const mentions = nodesOf(tree, 'mention')
    expect(mentions).toHaveLength(1)
    expect(mentions[0].data).toEqual({
      hName: 'span',
      hProperties: {
        className: ['kk-mention'],
        dataMentionUid: 'us1',
        dataMentionName: 'Anna Nová',
      },
    })
    expect(mentions[0].children).toEqual([{ type: 'text', value: '@Anna Nová' }])
    expect(nodesOf(tree, 'text').map((n) => n.value)).toEqual(['Hele ', '@Anna Nová', ', víš to?'])
    expect(nodesOf(tree, 'link')).toHaveLength(0)
  })

  it('drops an @ that becomes empty text and handles a mention at the start', () => {
    const tree = parse('@[Anna](user:us1) ahoj')
    expect(nodesOf(tree, 'text').map((n) => n.value)).toEqual(['@Anna', ' ahoj'])
  })

  it('leaves a user: link without the @, a bare @name and a mention in code alone', () => {
    expect(nodesOf(parse('[Anna](user:us1)'), 'mention')).toHaveLength(0)
    expect(nodesOf(parse('[Anna](user:us1)'), 'link')).toHaveLength(1)
    expect(nodesOf(parse('@Anna'), 'mention')).toHaveLength(0)
    expect(nodesOf(parse('`@[Anna](user:us1)`'), 'mention')).toHaveLength(0)
    expect(nodesOf(parse('```\n@[Anna](user:us1)\n```'), 'mention')).toHaveLength(0)
  })

  it('refuses a uid outside the account alphabet', () => {
    expect(nodesOf(parse('@[Anna](user:us/1)'), 'mention')).toHaveLength(0)
  })

  it('finds a mention nested in emphasis and a list', () => {
    const tree = parse('- **@[Anna](user:us1)** ano')
    expect(nodesOf(tree, 'mention')).toHaveLength(1)
  })
})

describe('mentionSanitizeSchema', () => {
  it('widens the default schema for the mention span only', () => {
    expect(mentionSanitizeSchema.attributes?.span).toEqual([
      ['className', 'kk-mention'],
      'dataMentionUid',
      'dataMentionName',
    ])
    expect(mentionSanitizeSchema.attributes?.a).toContain('href')
    expect(mentionSanitizeSchema.tagNames).not.toContain('script')
  })
})

describe('findMentionQuery', () => {
  it.each<[string, number, ReturnType<typeof findMentionQuery>]>([
    ['@', 1, { start: 0, query: '' }],
    ['ahoj @To', 8, { start: 5, query: 'To' }],
    ['ahoj @Tomáš Ko', 14, { start: 5, query: 'Tomáš Ko' }],
    ['(@an', 4, { start: 1, query: 'an' }],
    ['ahoj @To', 6, { start: 5, query: '' }],
    ['ahoj @To', 4, null],
    ['mail@example', 12, null],
    ['@ ano', 5, null],
    ['@[Anna](user:us1) ', 18, null],
    ['@Anna\nne', 8, null],
    ['ahoj @' + 'x'.repeat(41), 47, null],
  ])('%j at %i → %j', (text, caret, want) => {
    expect(findMentionQuery(text, caret)).toEqual(want)
  })
})

describe('filterMentionCandidates', () => {
  const users = [
    { uid: 'u1', name: 'Anna Nováková' },
    { uid: 'u2', name: 'Petr Kolář' },
    { uid: 'u3', name: 'Jana Kolářová' },
    { uid: 'u4', name: 'Bohumil Nedvěd' },
  ]

  it('matches accent- and case-insensitively, word starts first', () => {
    expect(filterMentionCandidates(users, 'kol').map((u) => u.uid)).toEqual(['u2', 'u3'])
    expect(filterMentionCandidates(users, 'PETR').map((u) => u.uid)).toEqual(['u2'])
    expect(filterMentionCandidates(users, 'na').map((u) => u.uid)).toEqual(['u1', 'u3'])
    expect(filterMentionCandidates(users, 'ko').map((u) => u.uid)).toEqual(['u2', 'u3', 'u1'])
    expect(filterMentionCandidates(users, 'xyz')).toEqual([])
  })

  it('offers the first names for an empty query, capped', () => {
    const many = Array.from({ length: 20 }, (_, i) => ({ uid: `u${i}`, name: `Osoba ${i}` }))
    expect(filterMentionCandidates(many, '')).toHaveLength(MAX_MENTION_SUGGESTIONS)
    expect(filterMentionCandidates(many, 'osoba')).toHaveLength(MAX_MENTION_SUGGESTIONS)
  })
})

describe('insertMention', () => {
  it('replaces the typed @query with the token and a space, caret after it', () => {
    const text = 'Hele @Pet víš?'
    const at = findMentionQuery(text, 9)
    if (at === null) {
      throw new Error('the caret sits in a mention query')
    }
    const got = insertMention(text, at, 9, {
      uid: 'us1',
      name: 'Petr Kolář',
    })
    expect(got.text).toBe('Hele @[Petr Kolář](user:us1)  víš?')
    expect(got.caret).toBe('Hele @[Petr Kolář](user:us1) '.length)
  })
})

describe('commentAnchorId', () => {
  it('names the element the #comment-<uid> link scrolls to', () => {
    expect(commentAnchorId('cm1')).toBe('comment-cm1')
  })
})
