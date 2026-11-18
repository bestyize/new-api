/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/
import { describe, expect, it, vi } from 'vitest'

import { runTest, silentWav, type TestEnvironment } from '../lib/run-test'

function environment(answer: Response | (() => Response), overrides: Partial<TestEnvironment> = {}) {
  const fetch = vi.fn(async () => (typeof answer === 'function' ? answer() : answer))
  let clock = 0
  const env: TestEnvironment = {
    origin: 'https://gateway.example',
    fetch: fetch as unknown as typeof globalThis.fetch,
    WebSocket: globalThis.WebSocket,
    now: () => (clock += 100),
    createObjectURL: () => 'blob:audio',
    ...overrides,
  }
  return { env, fetch }
}

const target = { model: 'tts-1', channelId: 7, tokenKey: 'abc' }
const jsonAnswer = (body: unknown, status = 200) =>
  new Response(JSON.stringify(body), { status, headers: { 'Content-Type': 'application/json' } })

describe('runTest', () => {
  it('pins the request to the channel with the admin token and sends what a client would', async () => {
    const { env, fetch } = environment(jsonAnswer({ results: [{ title: 'New API' }] }))

    const outcome = await runTest({ ...target, capability: 'search', model: 'deepseek-search' }, env)

    expect(outcome).toMatchObject({ ok: true, detail: 'New API', elapsed: 100 })
    const [url, init] = fetch.mock.calls[0] as unknown as [string, RequestInit]
    expect(url).toBe('https://gateway.example/v1/tools/search')
    expect((init.headers as Record<string, string>).Authorization).toBe('Bearer sk-abc-7')
    expect(JSON.parse(String(init.body))).toEqual({ query: 'New API', count: 1 })
  })

  it('accepts audio for speech and offers it to play', async () => {
    const { env } = environment(new Response(new Uint8Array(2048), { headers: { 'Content-Type': 'audio/mpeg' } }))

    const outcome = await runTest({ ...target, capability: 'speech' }, env)

    expect(outcome).toMatchObject({ ok: true, detail: '2 KB', preview: { kind: 'audio', url: 'blob:audio' } })
  })

  it('uploads a silent recording for recognition and accepts an empty transcript', async () => {
    const { env, fetch } = environment(jsonAnswer({ text: '' }))

    const outcome = await runTest({ ...target, capability: 'transcription', model: 'whisper-1' }, env)

    expect(outcome).toMatchObject({ ok: true, detail: 'Silence recognized', translatable: true })
    const body = (fetch.mock.calls[0] as unknown as [string, RequestInit])[1].body as FormData
    expect(body.get('model')).toBe('whisper-1')
    expect((body.get('file') as File).type).toBe('audio/wav')
  })

  it('shows the upstream’s own reason when the call fails, untranslated', async () => {
    const { env } = environment(jsonAnswer({ error: { message: 'model tts-1 not supported by this channel' } }, 400))

    const outcome = await runTest({ ...target, capability: 'speech' }, env)

    expect(outcome).toEqual({ ok: false, elapsed: 100, detail: 'model tts-1 not supported by this channel' })
  })

  it('fails an answer that lacks what the capability must return', async () => {
    const { env } = environment(jsonAnswer({ data: [] }))

    const outcome = await runTest({ ...target, capability: 'image', model: 'gpt-image-1' }, env)

    expect(outcome).toMatchObject({ ok: false, detail: 'No image in the answer', translatable: true })
  })

  it('opens a realtime session with the key as a subprotocol and passes on session.created', async () => {
    const opened: { url: string; protocols: string[] }[] = []
    class FakeSocket extends EventTarget {
      constructor(url: string, protocols: string[]) {
        super()
        opened.push({ url, protocols })
        setTimeout(() => this.dispatchEvent(new MessageEvent('message', { data: JSON.stringify({ type: 'session.created' }) })), 0)
      }
      close() {}
    }
    const { env } = environment(jsonAnswer({}), { WebSocket: FakeSocket as unknown as typeof WebSocket })

    const outcome = await runTest({ ...target, capability: 'realtime', model: 'gpt-realtime' }, env)

    expect(outcome).toMatchObject({ ok: true, detail: 'session.created' })
    expect(opened[0].url).toBe('wss://gateway.example/v1/realtime?model=gpt-realtime')
    expect(opened[0].protocols).toContain('openai-insecure-api-key.sk-abc-7')
  })

  it('gives up on a realtime session that never opens', async () => {
    class SilentSocket extends EventTarget {
      close() {}
    }
    const { env } = environment(jsonAnswer({}), { WebSocket: SilentSocket as unknown as typeof WebSocket, realtimeTimeout: 10 })

    const outcome = await runTest({ ...target, capability: 'realtime', model: 'gpt-realtime' }, env)

    expect(outcome).toMatchObject({ ok: false, detail: 'No session within the time limit', translatable: true })
  })
})

describe('silentWav', () => {
  it('is a 16 kHz mono 16-bit WAV of the asked length', async () => {
    const wav = silentWav(1)
    const view = new DataView(await wav.arrayBuffer())

    expect(wav.size).toBe(44 + 32_000)
    expect(view.getUint32(24, true)).toBe(16_000)
    expect(view.getUint16(22, true)).toBe(1)
  })
})
