import { afterEach, describe, expect, it, vi } from 'vitest'

import { ApiError } from './auth'
import {
  codeFromInput,
  createUploadLink,
  extendUploadLink,
  fetchPublicUploadLink,
  linkUploader,
  publicLinkURL,
  newUploadLinkCode,
  restoreUploadLinkCode,
  revokeUploadLink,
  UploadLinkGoneError,
} from './uploadLinks'

vi.mock('./upload', () => ({ uploadFile: vi.fn() }))

const { uploadFile } = await import('./upload')

/** Stubs fetch with one JSON answer and returns the mock. */
function stubFetch(status: number, body: unknown) {
  const mock = vi.fn().mockResolvedValue(
    new Response(JSON.stringify(body), {
      status,
      headers: { 'Content-Type': 'application/json' },
    }),
  )
  vi.stubGlobal('fetch', mock)
  return mock
}

afterEach(() => {
  vi.unstubAllGlobals()
  vi.mocked(uploadFile).mockReset()
})

describe('fetchPublicUploadLink', () => {
  it('returns the description of a live link', async () => {
    const fetchMock = stubFetch(200, { title: 'Pouť', note: '', albums: [], labels: [] })
    await expect(fetchPublicUploadLink('Ab3dEf7h')).resolves.toMatchObject({ title: 'Pouť' })
    expect(fetchMock.mock.calls[0][0]).toBe('/api/v1/u/Ab3dEf7h')
  })

  it('throws UploadLinkGoneError carrying the state for a dead link', async () => {
    stubFetch(410, { error: 'gone', state: 'revoked' })
    const err: unknown = await fetchPublicUploadLink('Ab3dEf7h').catch((e: unknown) => e)
    expect(err).toBeInstanceOf(UploadLinkGoneError)
    expect((err as UploadLinkGoneError).state).toBe('revoked')
  })

  it('throws ApiError(404) for an unknown code', async () => {
    stubFetch(404, { error: 'uploadlink: link not found' })
    const err: unknown = await fetchPublicUploadLink('Zz9Zz9Zz').catch((e: unknown) => e)
    expect(err).toBeInstanceOf(ApiError)
    expect((err as ApiError).status).toBe(404)
  })
})

describe('management calls', () => {
  it('creates, extends and revokes through the management routes', async () => {
    const fetchMock = stubFetch(201, {
      link: { uid: 'ul1' },
      code: 'Ab3dEf7h',
      path: '/u/Ab3dEf7h',
    })
    const created = await createUploadLink({
      title: 'x',
      note: '',
      album_uids: ['al1'],
      label_uids: [],
      valid_days: 7,
    })
    expect(created.code).toBe('Ab3dEf7h')
    expect(fetchMock.mock.calls[0][0]).toBe('/api/v1/upload-links')

    const extendFetch = stubFetch(200, { link: { uid: 'ul1', state: 'active' } })
    await expect(extendUploadLink('ul1', 14)).resolves.toMatchObject({ uid: 'ul1' })
    expect(extendFetch.mock.calls[0][0]).toBe('/api/v1/upload-links/ul1/extend')
    const init = extendFetch.mock.calls[0][1] as RequestInit
    expect(JSON.parse(init.body as string)).toEqual({ valid_days: 14 })

    const revokeFetch = stubFetch(200, { link: { uid: 'ul1', state: 'revoked' } })
    await expect(revokeUploadLink('ul1')).resolves.toMatchObject({ state: 'revoked' })
    expect(revokeFetch.mock.calls[0][0]).toBe('/api/v1/upload-links/ul1/revoke')
  })

  it('restores a code and draws a new one through the code routes', async () => {
    const restoreFetch = stubFetch(200, {
      link: { uid: 'ul1', code: 'Ab3dEf7h', path: '/u/Ab3dEf7h' },
    })
    await expect(restoreUploadLinkCode('ul1', 'Ab3dEf7h')).resolves.toMatchObject({
      path: '/u/Ab3dEf7h',
    })
    expect(restoreFetch.mock.calls[0][0]).toBe('/api/v1/upload-links/ul1/restore-code')
    const init = restoreFetch.mock.calls[0][1] as RequestInit
    expect(JSON.parse(init.body as string)).toEqual({ code: 'Ab3dEf7h' })

    const newFetch = stubFetch(200, { link: { uid: 'ul1', code: 'Nw5cDe9k', path: '/u/Nw5cDe9k' } })
    await expect(newUploadLinkCode('ul1')).resolves.toMatchObject({ code: 'Nw5cDe9k' })
    expect(newFetch.mock.calls[0][0]).toBe('/api/v1/upload-links/ul1/new-code')
  })

  it('surfaces the backend message as an ApiError', async () => {
    stubFetch(400, { error: 'uploadlink: a link needs at least one album or label' })
    await expect(
      createUploadLink({ title: '', note: '', album_uids: [], label_uids: [], valid_days: 7 }),
    ).rejects.toThrow('at least one album')
  })
})

describe('linkUploader', () => {
  it('posts each file to the link with the name read when the file starts', async () => {
    vi.mocked(uploadFile).mockResolvedValue({ filename: 'a', status: 201, outcome: 'created' })
    let name = ' Jana '
    const upload = linkUploader('Ab3dEf7h', () => name)
    const file = new File(['a'], 'a.jpg')
    await upload(file)
    name = 'Pepa'
    await upload(file)
    expect(vi.mocked(uploadFile).mock.calls[0][1]).toMatchObject({
      url: '/api/v1/u/Ab3dEf7h/upload',
      fields: { name: 'Jana' },
    })
    expect(vi.mocked(uploadFile).mock.calls[1][1]).toMatchObject({ fields: { name: 'Pepa' } })
  })
})

describe('publicLinkURL', () => {
  it('resolves a path against this origin', () => {
    expect(publicLinkURL('/u/Ab3dEf7h')).toBe(`${window.location.origin}/u/Ab3dEf7h`)
  })
})

describe('codeFromInput', () => {
  it.each([
    ['Ab3dEf7h', 'Ab3dEf7h'],
    ['  Ab3dEf7h ', 'Ab3dEf7h'],
    ['https://fotky.example/u/Ab3dEf7h', 'Ab3dEf7h'],
    ['https://fotky.example/u/Ab3dEf7h/?x=1#top', 'Ab3dEf7h'],
    ['', ''],
  ])('reads %j as %j', (input, want) => {
    expect(codeFromInput(input)).toBe(want)
  })
})
