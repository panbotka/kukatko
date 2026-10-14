import { beforeEach, describe, expect, it } from 'vitest'

import i18n from '../i18n'
import type { NotificationRecord } from '../services/notifications'
import { notificationText } from './notificationText'

/** A notification stored in Czech, as the server writes every one. */
function record(over: Partial<NotificationRecord>): NotificationRecord {
  return {
    uid: 'nt1',
    kind: 'tagged',
    title: 'Označili vás na fotkách',
    body: 'Přibyly 3 fotky, na kterých vás označili.',
    link: '/p/v/n/nt1',
    created_at: '2026-10-14T10:00:00Z',
    read_at: null,
    ...over,
  }
}

beforeEach(async () => {
  await i18n.changeLanguage('en')
})

describe('notificationText', () => {
  it('says a Czech-stored notification in English from its params', () => {
    expect(notificationText(record({ params: { count: 3 } }), i18n.t)).toEqual({
      title: 'You were tagged in photos',
      body: 'You were tagged in 3 photos.',
    })
  })

  it('renders Czech with all three plural forms', async () => {
    await i18n.changeLanguage('cs')
    const say = (count: number) => notificationText(record({ params: { count } }), i18n.t)
    expect(say(1)).toEqual({
      title: 'Označili vás na fotce',
      body: 'Přibyla 1 fotka, na které vás označili.',
    })
    expect(say(3).body).toBe('Přibyly 3 fotky, na kterých vás označili.')
    expect(say(12).body).toBe('Přibylo 12 fotek, na kterých vás označili.')
  })

  it('keeps the stored text when there are no params', () => {
    expect(notificationText(record({ params: null }), i18n.t).title).toBe('Označili vás na fotkách')
    expect(notificationText(record({}), i18n.t).title).toBe('Označili vás na fotkách')
  })

  it('keeps the stored text for a kind or variant it does not know', () => {
    expect(
      notificationText(record({ kind: 'future_kind', params: { count: 1 } }), i18n.t).title,
    ).toBe('Označili vás na fotkách')
    expect(
      notificationText(record({ params: { count: 1, variant: 'future' } }), i18n.t).title,
    ).toBe('Označili vás na fotkách')
  })

  it('names the album, and says untitled for a blank one', () => {
    const titled = notificationText(
      record({ params: { variant: 'album', count: 1, album: 'Pouť' } }),
      i18n.t,
    )
    expect(titled.title).toBe('You were tagged in a photo in the album Pouť')
    const blank = notificationText(record({ kind: 'album_featured', params: {} }), i18n.t)
    // An empty params object is still params: the album is simply untitled.
    expect(blank.title).toBe('New featured album: (untitled)')
    expect(notificationText(record({ params: { variant: 'other', count: 2 } }), i18n.t).title).toBe(
      'You were tagged in other photos',
    )
  })

  it('renders the people kinds, with somebody for a blank name', () => {
    expect(
      notificationText(
        record({ kind: 'task_assigned', params: { name: 'Anna', title: 'Kdy?' } }),
        i18n.t,
      ),
    ).toEqual({ title: 'Anna added you to the task: Kdy?', body: 'Open the task and answer.' })
    expect(
      notificationText(record({ kind: 'task_assigned', params: { title: 'Kdy?' } }), i18n.t).title,
    ).toBe('Somebody added you to the task: Kdy?')
    expect(
      notificationText(record({ kind: 'registration_pending', params: { name: 'Jan' } }), i18n.t)
        .body,
    ).toBe('Jan has just registered in Kukátko.')
  })

  it('keeps the comment itself as the body of a mention', () => {
    const base = { kind: 'comment_mention', body: 'Hele, to jsi ty!' }
    expect(
      notificationText(record({ ...base, params: { name: 'Anna', place: 'photo' } }), i18n.t),
    ).toEqual({
      title: 'Anna mentioned you in a comment on a photo',
      body: 'Hele, to jsi ty!',
    })
    expect(
      notificationText(
        record({ ...base, params: { name: 'Anna', place: 'album', title: 'Svatba' } }),
        i18n.t,
      ).title,
    ).toBe('Anna mentioned you in a comment on the album Svatba')
    expect(
      notificationText(
        record({ ...base, params: { name: 'Anna', place: 'task', title: 'Kdy?' } }),
        i18n.t,
      ).title,
    ).toBe('Anna mentioned you in a comment on the task Kdy?')
  })
})
