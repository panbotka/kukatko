import { afterEach, describe, expect, it, vi } from 'vitest'

import { ApiError } from './auth'
import { isAbortError, uploadFile, type UploadResponse } from './upload'

/**
 * Minimal XMLHttpRequest stand-in: jsdom's XHR cannot reach a network, so this
 * records the request and exposes helpers to drive the lifecycle (progress,
 * load, error, abort) from the test.
 */
class FakeXHR {
  static instances: FakeXHR[] = []

  upload = { onprogress: null as ((event: ProgressEvent) => void) | null }
  onload: (() => void) | null = null
  onerror: (() => void) | null = null
  onabort: (() => void) | null = null
  status = 0
  statusText = ''
  response: unknown = null
  responseType = ''
  withCredentials = false
  method = ''
  url = ''
  sent: unknown = undefined
  aborted = false

  constructor() {
    FakeXHR.instances.push(this)
  }

  open(method: string, url: string): void {
    this.method = method
    this.url = url
  }

  setRequestHeader(): void {
    // no-op
  }

  send(body: unknown): void {
    this.sent = body
  }

  abort(): void {
    this.aborted = true
    this.onabort?.()
  }

  // Test drivers --------------------------------------------------------------

  emitProgress(loaded: number, total: number): void {
    this.upload.onprogress?.({ lengthComputable: true, loaded, total } as ProgressEvent)
  }

  respond(status: number, response: unknown): void {
    this.status = status
    this.response = response
    this.onload?.()
  }

  failNetwork(): void {
    this.onerror?.()
  }
}

function installFakeXHR(): typeof FakeXHR {
  FakeXHR.instances = []
  vi.stubGlobal('XMLHttpRequest', FakeXHR)
  return FakeXHR
}

function file(name = 'a.jpg'): File {
  return new File(['data'], name, { type: 'image/jpeg' })
}

function created(name = 'a.jpg'): UploadResponse {
  return { results: [{ filename: name, status: 201, outcome: 'created', photo_uid: 'ph1' }] }
}

afterEach(() => {
  vi.unstubAllGlobals()
})

describe('uploadFile', () => {
  it('posts a multipart body to the upload endpoint and resolves the result', async () => {
    const xhrs = installFakeXHR()
    const promise = uploadFile(file('a.jpg'))

    const xhr = xhrs.instances[0]
    expect(xhr.method).toBe('POST')
    expect(xhr.url).toBe('/api/v1/upload')
    expect(xhr.sent).toBeInstanceOf(FormData)

    xhr.respond(200, created('a.jpg'))

    await expect(promise).resolves.toMatchObject({ outcome: 'created', photo_uid: 'ph1' })
  })

  it('reports upload progress as a fraction', async () => {
    const xhrs = installFakeXHR()
    const onProgress = vi.fn()
    const promise = uploadFile(file(), { onProgress })

    const xhr = xhrs.instances[0]
    xhr.emitProgress(50, 100)
    expect(onProgress).toHaveBeenLastCalledWith(0.5)
    xhr.emitProgress(100, 100)
    expect(onProgress).toHaveBeenLastCalledWith(1)

    xhr.respond(200, created())
    await promise
  })

  it('resolves a duplicate outcome without throwing', async () => {
    const xhrs = installFakeXHR()
    const promise = uploadFile(file())
    xhrs.instances[0].respond(200, {
      results: [{ filename: 'a.jpg', status: 409, outcome: 'duplicate', photo_uid: 'ph9' }],
    })
    await expect(promise).resolves.toMatchObject({ outcome: 'duplicate' })
  })

  it('surfaces a per-file error outcome with warnings', async () => {
    const xhrs = installFakeXHR()
    const promise = uploadFile(file())
    xhrs.instances[0].respond(200, {
      results: [
        {
          filename: 'a.jpg',
          status: 201,
          outcome: 'created',
          photo_uid: 'ph2',
          warnings: [{ code: 'near_duplicate', message: 'similar', photo_uid: 'ph3' }],
        },
      ],
    })
    await expect(promise).resolves.toMatchObject({
      warnings: [{ code: 'near_duplicate' }],
    })
  })

  it('throws ApiError carrying the status on a non-OK response', async () => {
    const xhrs = installFakeXHR()
    const promise = uploadFile(file())
    xhrs.instances[0].respond(413, { error: 'file too large' })

    await expect(promise).rejects.toBeInstanceOf(ApiError)
    await expect(promise).rejects.toMatchObject({ status: 413, message: 'file too large' })
  })

  it('throws ApiError on a network error', async () => {
    const xhrs = installFakeXHR()
    const promise = uploadFile(file())
    xhrs.instances[0].failNetwork()
    await expect(promise).rejects.toMatchObject({ name: 'ApiError', status: 0 })
  })

  it('rejects with an AbortError when the signal is aborted', async () => {
    const xhrs = installFakeXHR()
    const controller = new AbortController()
    const promise = uploadFile(file(), { signal: controller.signal })

    controller.abort()
    expect(xhrs.instances[0].aborted).toBe(true)
    await expect(promise).rejects.toSatisfy(isAbortError)
  })

  it('rejects immediately if the signal is already aborted', async () => {
    installFakeXHR()
    const controller = new AbortController()
    controller.abort()
    await expect(uploadFile(file(), { signal: controller.signal })).rejects.toSatisfy(isAbortError)
  })
})
