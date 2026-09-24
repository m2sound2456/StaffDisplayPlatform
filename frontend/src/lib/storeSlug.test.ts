import { describe, expect, it } from 'vitest'

import {
  defaultStoreSlug,
  isValidStoreSlug,
  normalizeStoreSlug,
  storeDisplayPath,
  storeDisplayUrl,
  validateStoreSlug,
} from '@/lib/storeSlug'

describe('normalizeStoreSlug', () => {
  it('lowercases, collapses separators and trims hyphens', () => {
    expect(normalizeStoreSlug('  My  Coffee Shop!! ')).toBe('my-coffee-shop')
    expect(normalizeStoreSlug('--abc--')).toBe('abc')
    expect(normalizeStoreSlug(undefined)).toBe('')
  })
})

describe('validateStoreSlug', () => {
  it.each(['abc', 'coffee', 'shop001', 'abc-123', 'a', 'a-b-c'])('accepts %s', (slug) => {
    expect(isValidStoreSlug(slug)).toBe(true)
  })

  it.each([
    ['', 'required'],
    ['   ', 'required'],
    ['ABC', 'lowercase'],
    ['coffee shop', 'only contain'],
    ['-abc', 'only contain'],
    ['abc-', 'only contain'],
    ['.abc', 'only contain'],
    ['a'.repeat(64), '63 characters'],
  ])('rejects %s', (slug, reason) => {
    const result = validateStoreSlug(slug)
    expect(result.valid).toBe(false)
    expect(result.reason ?? '').toContain(reason)
  })

  it.each(['app', 'setup', 'api', 'ws', 'healthz', 'readyz', 'assets', 'icons', 'static'])(
    'rejects the reserved slug %s',
    (slug) => {
      const result = validateStoreSlug(slug)
      expect(result.valid).toBe(false)
      expect(result.reason).toContain('reserved')
    },
  )

  it('accepts a 63 character slug', () => {
    const slug = 'a'.repeat(62) + 'b'
    expect(slug).toHaveLength(63)
    expect(isValidStoreSlug(slug)).toBe(true)
  })

  it('accepts repeated hyphens from user input but normalises them away', () => {
    // The stored pattern allows interior hyphens; the admin flow normalises
    // input ("abc  def" -> "abc-def") before it reaches the API.
    expect(isValidStoreSlug('abc--def')).toBe(true)
    expect(normalizeStoreSlug('abc--def')).toBe('abc-def')
  })
})

describe('store display helpers', () => {
  it('builds the canonical path and absolute url', () => {
    expect(storeDisplayPath('coffee')).toBe('/s/coffee')
    expect(storeDisplayUrl('https://display.example.com', 'coffee')).toBe('https://display.example.com/s/coffee')
  })

  it('falls back to the demo slug when nothing is configured', () => {
    expect(defaultStoreSlug()).toBe('demo')
  })
})
