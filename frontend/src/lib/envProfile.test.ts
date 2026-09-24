import { describe, expect, it } from 'vitest'

import { DEFAULT_API_BASE_URL, resolveApiBaseUrl } from '@/lib/envProfile'

describe('resolveApiBaseUrl', () => {
  it('falls back to the same-origin default when nothing is configured', () => {
    expect(resolveApiBaseUrl(undefined, 'development')).toBe(DEFAULT_API_BASE_URL)
    expect(resolveApiBaseUrl('   ', 'production')).toBe(DEFAULT_API_BASE_URL)
    expect(resolveApiBaseUrl('/', 'staging')).toBe(DEFAULT_API_BASE_URL)
  })

  it('normalises trailing slashes', () => {
    expect(resolveApiBaseUrl('/api/v1/', 'production')).toBe('/api/v1')
  })

  it('accepts a relative base in every profile', () => {
    for (const mode of ['development', 'staging', 'production', 'test']) {
      expect(resolveApiBaseUrl('/api/v1', mode)).toBe('/api/v1')
    }
  })

  it('keeps a local development host usable', () => {
    expect(resolveApiBaseUrl('http://127.0.0.1:8080/api/v1', 'development')).toBe('http://127.0.0.1:8080/api/v1')
    expect(resolveApiBaseUrl('http://localhost:8080', 'development')).toBe('http://localhost:8080')
  })

  it('rejects an absolute API base outside development', () => {
    for (const mode of ['staging', 'production']) {
      expect(() => resolveApiBaseUrl('https://api.example.com/v1', mode)).toThrow(/same-origin path/)
    }
  })

  it('rejects a remote host even in development', () => {
    expect(() => resolveApiBaseUrl('https://api.example.com', 'development')).toThrow(/same-origin path/)
    expect(() => resolveApiBaseUrl('ftp://127.0.0.1', 'development')).toThrow(/same-origin path/)
  })

  it('rejects protocol relative and malformed values', () => {
    expect(() => resolveApiBaseUrl('//api.example.com', 'production')).toThrow(/protocol relative/)
    expect(() => resolveApiBaseUrl('/api/v1?x=1', 'production')).toThrow(/query or fragment/)
    expect(() => resolveApiBaseUrl('/api/v1#top', 'production')).toThrow(/query or fragment/)
    expect(() => resolveApiBaseUrl('/api v1', 'production')).toThrow(/whitespace/)
  })
})
