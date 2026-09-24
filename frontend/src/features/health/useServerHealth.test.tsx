import { act, renderHook, waitFor } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'

import { useServerHealth } from '@/features/health/useServerHealth'
import { dataResponse, errorResponse, healthFixture, stubFetch, stubHealthApi, versionFixture } from '@/test/mocks'

describe('useServerHealth', () => {
  it('reports the platform status once both requests succeed', async () => {
    stubHealthApi()

    const { result } = renderHook(() => useServerHealth())

    expect(result.current.phase).toBe('loading')

    await waitFor(() => expect(result.current.phase).toBe('ready'))
    expect(result.current.health).toEqual(healthFixture)
    expect(result.current.version).toEqual(versionFixture)
    expect(result.current.error).toBeNull()
  })

  it('surfaces a transport failure', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn(() => Promise.reject(new TypeError('network down'))),
    )

    const { result } = renderHook(() => useServerHealth())

    await waitFor(() => expect(result.current.phase).toBe('error'))
    expect(result.current.error).toContain('unreachable')
  })

  it('surfaces API error codes', async () => {
    stubFetch((url) =>
      url.includes('/version')
        ? dataResponse(versionFixture)
        : errorResponse(503, 'service_unavailable', 'database unreachable'),
    )

    const { result } = renderHook(() => useServerHealth())

    await waitFor(() => expect(result.current.phase).toBe('error'))
    expect(result.current.error).toBe('service_unavailable: database unreachable')
  })

  it('marks a degraded database report as ready data', async () => {
    stubHealthApi({
      health: dataResponse({
        ...healthFixture,
        status: 'degraded',
        checks: { database: { status: 'unavailable', message: 'database unreachable' } },
      }),
    })

    const { result } = renderHook(() => useServerHealth())

    await waitFor(() => expect(result.current.phase).toBe('ready'))
    expect(result.current.health?.status).toBe('degraded')
    expect(result.current.health?.checks['database']?.status).toBe('unavailable')
  })

  it('refresh issues new requests', async () => {
    const mock = stubHealthApi()

    const { result } = renderHook(() => useServerHealth())
    await waitFor(() => expect(result.current.phase).toBe('ready'))

    const callsBefore = mock.mock.calls.length

    act(() => {
      result.current.refresh()
    })

    await waitFor(() => expect(mock.mock.calls.length).toBeGreaterThan(callsBefore))
    await waitFor(() => expect(result.current.phase).toBe('ready'))
  })

  it('polls when an interval is configured', async () => {
    const mock = stubHealthApi()

    const { result } = renderHook(() => useServerHealth(30))
    await waitFor(() => expect(result.current.phase).toBe('ready'))

    const callsBefore = mock.mock.calls.length

    await waitFor(() => expect(mock.mock.calls.length).toBeGreaterThan(callsBefore), { timeout: 2000 })
  })
})
