import { describe, expect, it, vi } from 'vitest'

import { ApiError, api, apiFetch, buildApiUrl } from '@/services/apiClient'
import { dataResponse, errorResponse, jsonResponse, stubFetch } from '@/test/mocks'

describe('buildApiUrl', () => {
  it('joins the base url with the path', () => {
    expect(buildApiUrl('/health')).toBe('/api/v1/health')
    expect(buildApiUrl('stores')).toBe('/api/v1/stores')
  })

  it('serialises defined query values and skips empty ones', () => {
    expect(buildApiUrl('/stores', { page: 1, q: 'coffee', skip: undefined, empty: '' })).toBe(
      '/api/v1/stores?page=1&q=coffee',
    )
  })
})

describe('apiFetch', () => {
  it('unwraps the data envelope', async () => {
    stubFetch(() => dataResponse({ status: 'ok' }))

    await expect(apiFetch<{ status: string }>('/health')).resolves.toEqual({ status: 'ok' })
  })

  it('sends JSON bodies for POST requests', async () => {
    const mock = stubFetch(() => dataResponse({ id: 'store-1' }))

    await api.post('/stores', { name: 'Coffee' })

    const [url, init] = mock.mock.calls[0] as [string, RequestInit]
    expect(url).toBe('/api/v1/stores')
    expect(init.method).toBe('POST')
    expect(init.body).toBe('{"name":"Coffee"}')
    expect((init.headers as Record<string, string>)['Content-Type']).toBe('application/json')
    expect(init.credentials).toBe('same-origin')
  })

  it('uses the requested HTTP verb and adds no body for GET', async () => {
    const mock = stubFetch(() => dataResponse([]))

    await api.delete('/employees/1')

    const [, init] = mock.mock.calls[0] as [string, RequestInit]
    expect(init.method).toBe('DELETE')
    expect(init.body).toBeUndefined()
  })

  it('throws ApiError built from the error envelope', async () => {
    stubFetch(() => errorResponse(404, 'not_found', 'resource not found'))

    const error = await apiFetch('/stores/other-tenant').catch((caught: unknown) => caught)

    expect(error).toBeInstanceOf(ApiError)
    const apiError = error as ApiError
    expect(apiError.status).toBe(404)
    expect(apiError.code).toBe('not_found')
    expect(apiError.message).toBe('resource not found')
    expect(apiError.isNetworkError).toBe(false)
  })

  it('falls back to unknown_error for non JSON failures', async () => {
    stubFetch(() => jsonResponse('gateway exploded', 502))

    const error = (await apiFetch('/health').catch((caught: unknown) => caught)) as ApiError

    expect(error).toBeInstanceOf(ApiError)
    expect(error.code).toBe('unknown_error')
    expect(error.status).toBe(502)
  })

  it('maps transport failures to a network_error', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn(() => Promise.reject(new TypeError('network down'))),
    )

    const error = (await apiFetch('/health').catch((caught: unknown) => caught)) as ApiError

    expect(error).toBeInstanceOf(ApiError)
    expect(error.isNetworkError).toBe(true)
    expect(error.message).toMatch(/unable to reach/i)
  })

  it('propagates aborts', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn(() => Promise.reject(new DOMException('aborted', 'AbortError'))),
    )

    const error = (await apiFetch('/health').catch((caught: unknown) => caught)) as Error

    expect(error).toBeInstanceOf(DOMException)
    expect(error.name).toBe('AbortError')
  })

  it('returns raw payloads that are not enveloped', async () => {
    stubFetch(() => jsonResponse({ plain: true }))

    await expect(apiFetch<{ plain: boolean }>('/plain')).resolves.toEqual({ plain: true })
  })
})
