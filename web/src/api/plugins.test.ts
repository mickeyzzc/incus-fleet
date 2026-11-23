import { describe, expect, it } from 'vitest'

import { ApiClient } from './client'

/** Records requests so the new plugin/token methods can be asserted precisely. */
interface Call {
  method: string
  url: string
  body?: string
}

function clientWithRecorder(): { client: ApiClient; calls: Call[]; last: () => Call } {
  const calls: Call[] = []
  const client = new ApiClient({
    fetchImpl: async (input, init) => {
      calls.push({
        method: init?.method ?? 'GET',
        url: String(input),
        body: typeof init?.body === 'string' ? init.body : undefined,
      })

      return new Response(JSON.stringify({ tokens: [] }), {
        status: 200,
        headers: { 'Content-Type': 'application/json' },
      })
    },
  })

  return {
    client,
    calls,
    // Narrow the element once so assertions read cleanly under strict TS.
    last: () => {
      const call = calls[calls.length - 1]
      if (!call) throw new Error('no request was recorded')
      return call
    },
  }
}

describe('plugin and token client methods', () => {
  it('lists plugins', async () => {
    const { client, calls, last } = clientWithRecorder()

    await client.listPlugins()

    expect(calls).toHaveLength(1)
    expect(last().method).toBe('GET')
    expect(last().url).toContain('/api/v1/plugins')
  })

  it('loads one plugin by id', async () => {
    const { client, last } = clientWithRecorder()

    await client.getPlugin('victoria metrics')

    expect(last().url).toContain('/api/v1/plugins/victoria%20metrics')
  })

  it('issues a token with the requested scopes', async () => {
    const { client, last } = clientWithRecorder()

    await client.createAPIToken({ name: 'hive', scopes: ['read:clusters'] })

    expect(last().method).toBe('POST')
    expect(last().url).toContain('/api/v1/tokens')
    expect(last().body).toContain('"name":"hive"')
    expect(last().body).toContain('read:clusters')
  })

  it('revokes a token by id', async () => {
    const { client, last } = clientWithRecorder()

    await client.revokeAPIToken('abc/123')

    expect(last().method).toBe('DELETE')
    expect(last().url).toContain('/api/v1/tokens/abc%2F123')
  })

  it('lists tokens', async () => {
    const { client, last } = clientWithRecorder()

    await client.listAPITokens()

    expect(last().url).toContain('/api/v1/tokens')
    expect(last().method).toBe('GET')
  })

  it('reads the scope vocabulary', async () => {
    const { client, last } = clientWithRecorder()

    await client.tokenScopes()

    expect(last().url).toContain('/api/v1/tokens/scopes')
  })
})
