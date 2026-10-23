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
import type { CapabilityId } from './capabilities'

/**
 * A test sends what a client would send, to the public `/v1` API, with an
 * admin token pinned to one channel (`sk-<key>-<channelId>`), so it runs the
 * real relay, billing and logging of that channel.
 */
export type TestTarget = {
  capability: CapabilityId
  model: string
  channelId: number
  /** The admin token's key, without the `sk-` prefix. */
  tokenKey: string
}

export type TestPreview =
  | { kind: 'audio'; url: string }
  | { kind: 'image'; url: string }
  | { kind: 'text'; text: string }

export type TestOutcome = {
  ok: boolean
  /** Milliseconds from sending to the answer. */
  elapsed: number
  /** Why it failed, or a short note on what came back. */
  detail: string
  /** `detail` is an English i18n key of ours, not text from the upstream. */
  translatable?: boolean
  preview?: TestPreview
}

export type TestEnvironment = {
  origin: string
  fetch: typeof fetch
  WebSocket: typeof WebSocket
  now: () => number
  createObjectURL: (blob: Blob) => string
  /** How long a realtime session may take to open. */
  realtimeTimeout?: number
}

export const REALTIME_TIMEOUT = 15_000

/** One second of 16 kHz mono silence, the smallest recording a recognizer accepts. */
export function silentWav(seconds = 1, sampleRate = 16_000): Blob {
  const samples = seconds * sampleRate
  const buffer = new ArrayBuffer(44 + samples * 2)
  const view = new DataView(buffer)
  const text = (offset: number, value: string) => {
    for (let i = 0; i < value.length; i++) view.setUint8(offset + i, value.charCodeAt(i))
  }
  text(0, 'RIFF')
  view.setUint32(4, 36 + samples * 2, true)
  text(8, 'WAVE')
  text(12, 'fmt ')
  view.setUint32(16, 16, true)
  view.setUint16(20, 1, true)
  view.setUint16(22, 1, true)
  view.setUint32(24, sampleRate, true)
  view.setUint32(28, sampleRate * 2, true)
  view.setUint16(32, 2, true)
  view.setUint16(34, 16, true)
  text(36, 'data')
  view.setUint32(40, samples * 2, true)
  return new Blob([buffer], { type: 'audio/wav' })
}

async function errorText(response: Response): Promise<string> {
  const body = await response.text().catch(() => '')
  try {
    const parsed = JSON.parse(body) as { error?: { message?: string } | string; message?: string }
    if (typeof parsed.error === 'string') return parsed.error
    if (parsed.error?.message) return parsed.error.message
    if (parsed.message) return parsed.message
  } catch {
    // Not JSON: show the text itself.
  }
  return body.slice(0, 300) || `HTTP ${response.status}`
}

type Request = { path: string; body: BodyInit; json: boolean }

function requestFor(target: TestTarget): Request | null {
  const model = target.model
  const json = (value: unknown): Request['body'] => JSON.stringify(value)
  switch (target.capability) {
    case 'speech':
      return { path: '/v1/audio/speech', json: true, body: json({ model, input: '你好', voice: 'alloy' }) }
    case 'transcription': {
      const form = new FormData()
      form.append('model', model)
      form.append('file', silentWav(), 'silence.wav')
      return { path: '/v1/audio/transcriptions', json: false, body: form }
    }
    case 'image':
      return { path: '/v1/images/generations', json: true, body: json({ model, prompt: 'A red circle on a white background', n: 1, size: '1024x1024' }) }
    case 'search':
      return { path: '/v1/tools/search', json: true, body: json({ query: 'New API', count: 1 }) }
    case 'fetch':
      return { path: '/v1/tools/fetch', json: true, body: json({ url: 'https://example.com' }) }
    case 'embeddings':
      return { path: '/v1/embeddings', json: true, body: json({ model, input: 'hello' }) }
    case 'rerank':
      return { path: '/v1/rerank', json: true, body: json({ model, query: 'apple', documents: ['apple', 'banana'] }) }
    default:
      return null
  }
}

/** What a successful answer must contain, and what to show of it. */
async function readAnswer(capability: CapabilityId, response: Response, env: TestEnvironment): Promise<Omit<TestOutcome, 'elapsed'>> {
  if (capability === 'speech') {
    const type = response.headers.get('content-type') ?? ''
    if (type.includes('json')) return { ok: false, detail: await errorText(response) }
    const audio = await response.blob()
    if (audio.size === 0) return { ok: false, detail: 'Empty audio', translatable: true }
    return { ok: true, detail: `${Math.round(audio.size / 1024)} KB`, preview: { kind: 'audio', url: env.createObjectURL(audio) } }
  }
  const body = (await response.json().catch(() => null)) as Record<string, unknown> | null
  if (!body) return { ok: false, detail: 'Not a JSON answer', translatable: true }
  switch (capability) {
    case 'transcription':
      if (typeof body.text !== 'string') return { ok: false, detail: 'No text in the answer', translatable: true }
      return { ok: true, ...(body.text ? { detail: body.text.slice(0, 80) } : { detail: 'Silence recognized', translatable: true }) }
    case 'image': {
      const first = (body.data as { b64_json?: string; url?: string }[] | undefined)?.[0]
      const url = first?.b64_json ? `data:image/png;base64,${first.b64_json}` : first?.url
      if (!url) return { ok: false, detail: 'No image in the answer', translatable: true }
      return { ok: true, detail: 'Image generated', translatable: true, preview: { kind: 'image', url } }
    }
    case 'search': {
      const results = body.results as { title?: string }[] | undefined
      if (!Array.isArray(results)) return { ok: false, detail: 'No results in the answer', translatable: true }
      return results[0]?.title ? { ok: true, detail: results[0].title } : { ok: true, detail: 'No results', translatable: true }
    }
    case 'fetch': {
      if (typeof body.content !== 'string') return { ok: false, detail: 'No page content in the answer', translatable: true }
      return { ok: true, detail: String(body.title ?? ''), preview: { kind: 'text', text: body.content.slice(0, 500) } }
    }
    case 'embeddings': {
      const vector = (body.data as { embedding?: number[] }[] | undefined)?.[0]?.embedding
      if (!Array.isArray(vector)) return { ok: false, detail: 'No embedding in the answer', translatable: true }
      return { ok: true, detail: `${vector.length} dimensions` }
    }
    case 'rerank': {
      const results = body.results as unknown[] | undefined
      if (!Array.isArray(results)) return { ok: false, detail: 'No results in the answer', translatable: true }
      return { ok: true, detail: `${results.length} results` }
    }
    default:
      return { ok: false, detail: 'Not testable', translatable: true }
  }
}

/** Opens a realtime session and waits for the server to say it is ready. */
function testRealtime(target: TestTarget, env: TestEnvironment, started: number): Promise<TestOutcome> {
  const url = `${env.origin.replace(/^http/, 'ws')}/v1/realtime?model=${encodeURIComponent(target.model)}`
  const key = `sk-${target.tokenKey}-${target.channelId}`
  return new Promise((resolve) => {
    let settled = false
    const socket = new env.WebSocket(url, ['realtime', `openai-insecure-api-key.${key}`, 'openai-beta.realtime-v1'])
    const finish = (outcome: Omit<TestOutcome, 'elapsed'>) => {
      if (settled) return
      settled = true
      clearTimeout(timer)
      socket.close()
      resolve({ ...outcome, elapsed: Math.round(env.now() - started) })
    }
    const timer = setTimeout(() => finish({ ok: false, detail: 'No session within the time limit', translatable: true }), env.realtimeTimeout ?? REALTIME_TIMEOUT)
    socket.addEventListener('message', (event) => {
      let message: { type?: string; error?: { message?: string } } = {}
      try {
        message = JSON.parse(String(event.data))
      } catch {
        // Not an event: keep waiting.
        return
      }
      if (message.type === 'session.created') finish({ ok: true, detail: 'session.created' })
      else if (message.type === 'error') finish({ ok: false, detail: message.error?.message ?? 'error' })
    })
    socket.addEventListener('error', () => finish({ ok: false, detail: 'The connection failed', translatable: true }))
    socket.addEventListener('close', (event) => finish({ ok: false, detail: event.reason || `Closed (${event.code})` }))
  })
}

export async function runTest(target: TestTarget, env: TestEnvironment): Promise<TestOutcome> {
  const started = env.now()
  if (target.capability === 'realtime') return testRealtime(target, env, started)
  const request = requestFor(target)
  if (!request) return { ok: false, elapsed: 0, detail: 'Not testable', translatable: true }
  const headers: Record<string, string> = { Authorization: `Bearer sk-${target.tokenKey}-${target.channelId}` }
  if (request.json) headers['Content-Type'] = 'application/json'
  try {
    const response = await env.fetch(`${env.origin}${request.path}`, { method: 'POST', headers, body: request.body })
    if (!response.ok) return { ok: false, elapsed: Math.round(env.now() - started), detail: await errorText(response) }
    const answer = await readAnswer(target.capability, response, env)
    return { ...answer, elapsed: Math.round(env.now() - started) }
  } catch (error) {
    return { ok: false, elapsed: Math.round(env.now() - started), detail: error instanceof Error ? error.message : String(error) }
  }
}
