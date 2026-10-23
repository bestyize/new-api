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
export type CapabilityId =
  | 'speech'
  | 'transcription'
  | 'realtime'
  | 'image'
  | 'video'
  | 'search'
  | 'fetch'
  | 'embeddings'
  | 'rerank'

export type Capability = {
  id: CapabilityId
  /** i18n key of the section title. */
  title: string
  /** i18n key of the one-line explanation. */
  description: string
  method: 'POST' | 'WebSocket'
  path: string
  /** Endpoint types reported by `/api/pricing` that mean a model serves this capability. */
  endpointTypes: string[]
  /** Model-name hints for capabilities the endpoint types do not describe yet. */
  namePattern?: RegExp
  /** A routing name that always means this capability (DeepSeek Search / Fetch). */
  fixedModel?: string
  /** Whether a one-click test can run from the browser. */
  testable: boolean
  /** A minimal request a client would send, for the copyable example. */
  example: (model: string) => string
}

const json = (value: unknown) => JSON.stringify(value, null, 2)

export const CAPABILITIES: Capability[] = [
  {
    id: 'speech',
    title: 'Speech synthesis',
    description: 'Turns text into audio.',
    method: 'POST',
    path: '/v1/audio/speech',
    endpointTypes: [],
    namePattern: /tts|speech|cosyvoice|sambert/i,
    testable: true,
    example: (model) => json({ model, input: '你好', voice: 'alloy' }),
  },
  {
    id: 'transcription',
    title: 'Speech recognition',
    description: 'Turns an uploaded recording into text.',
    method: 'POST',
    path: '/v1/audio/transcriptions',
    endpointTypes: [],
    namePattern: /whisper|transcri|asr|sensevoice|paraformer|stt/i,
    testable: true,
    example: (model) => `multipart/form-data\nmodel=${model}\nfile=@recording.wav`,
  },
  {
    id: 'realtime',
    title: 'Realtime voice',
    description: 'A two-way voice conversation over WebSocket.',
    method: 'WebSocket',
    path: '/v1/realtime',
    endpointTypes: [],
    namePattern: /realtime/i,
    testable: true,
    example: (model) => `GET /v1/realtime?model=${model}\nAuthorization: Bearer sk-...`,
  },
  {
    id: 'image',
    title: 'Image generation',
    description: 'Creates images from a prompt.',
    method: 'POST',
    path: '/v1/images/generations',
    endpointTypes: ['image-generation'],
    testable: true,
    example: (model) =>
      json({ model, prompt: 'A red circle on white', n: 1, size: '1024x1024' }),
  },
  {
    id: 'video',
    title: 'Video generation',
    description: 'Creates videos as asynchronous tasks.',
    method: 'POST',
    path: '/v1/videos',
    endpointTypes: ['openai-video'],
    testable: false,
    example: (model) => json({ model, prompt: 'A cat walking', seconds: 4 }),
  },
  {
    id: 'search',
    title: 'Web search',
    description: 'Searches the web and returns titles, links and snippets.',
    method: 'POST',
    path: '/v1/tools/search',
    endpointTypes: ['deepseek-tools-search'],
    fixedModel: 'deepseek-search',
    testable: true,
    example: () => json({ query: 'New API', count: 5 }),
  },
  {
    id: 'fetch',
    title: 'Web page fetch',
    description: 'Returns a web page as Markdown.',
    method: 'POST',
    path: '/v1/tools/fetch',
    endpointTypes: ['deepseek-tools-fetch'],
    fixedModel: 'deepseek-fetch',
    testable: true,
    example: () => json({ url: 'https://example.com' }),
  },
  {
    id: 'embeddings',
    title: 'Embeddings',
    description: 'Turns text into vectors for search and retrieval.',
    method: 'POST',
    path: '/v1/embeddings',
    endpointTypes: ['embeddings'],
    namePattern: /embed|bge-|m3e/i,
    testable: true,
    example: (model) => json({ model, input: 'hello' }),
  },
  {
    id: 'rerank',
    title: 'Rerank',
    description: 'Orders documents by relevance to a query.',
    method: 'POST',
    path: '/v1/rerank',
    endpointTypes: ['jina-rerank'],
    namePattern: /rerank/i,
    testable: true,
    example: (model) =>
      json({ model, query: 'apple', documents: ['apple', 'banana'] }),
  },
]

/**
 * Whether a model looks like it serves a capability: by the endpoint types
 * the server reports, by a fixed routing name, or by its name for the
 * capabilities endpoint types do not describe. A guess for the name-based
 * ones; the test tells for sure.
 */
export function servesCapability(
  capability: Capability,
  model: string,
  endpointTypes: string[]
): boolean {
  if (capability.fixedModel) return model === capability.fixedModel
  if (endpointTypes.some((type) => capability.endpointTypes.includes(type))) {
    return true
  }
  return capability.namePattern?.test(model) ?? false
}
