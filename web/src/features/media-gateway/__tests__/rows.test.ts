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
import { describe, expect, it } from 'vitest'

import { buildRows } from '../lib/rows'

const channels = [
  { id: 1, name: 'OpenAI', status: 1, models: 'gpt-image-1, tts-1,whisper-1,gpt-realtime,gpt-5' },
  { id: 2, name: 'DeepSeek', status: 1, models: 'deepseek-search,deepseek-fetch,deepseek-v4' },
  { id: 3, name: 'Backup', status: 1, models: 'tts-1' },
  { id: 4, name: 'Off', status: 2, models: 'tts-1,bge-m3' },
]
const pricing = [
  { model_name: 'gpt-image-1', quota_type: 1, model_ratio: 0, model_price: 0.04, supported_endpoint_types: ['image-generation', 'openai'] },
  { model_name: 'tts-1', quota_type: 0, model_ratio: 7.5, supported_endpoint_types: ['openai'] },
  { model_name: 'deepseek-search', quota_type: 1, model_ratio: 0, model_price: 0.00075, supported_endpoint_types: ['deepseek-tools-search'] },
  { model_name: 'gpt-5', quota_type: 0, model_ratio: 0.625, supported_endpoint_types: ['openai'] },
]

describe('buildRows', () => {
  it('groups enabled channels’ models by capability, one row per channel', () => {
    const rows = buildRows(channels, pricing)

    expect(rows.speech.map((row) => [row.model, row.channel.id])).toEqual([
      ['tts-1', 1],
      ['tts-1', 3],
    ])
    expect(rows.transcription.map((row) => row.model)).toEqual(['whisper-1'])
    expect(rows.realtime.map((row) => row.model)).toEqual(['gpt-realtime'])
    expect(rows.image.map((row) => row.model)).toEqual(['gpt-image-1'])
    expect(rows.search.map((row) => row.model)).toEqual(['deepseek-search'])
    expect(rows.fetch.map((row) => row.model)).toEqual(['deepseek-fetch'])
  })

  it('leaves chat models and disabled channels out', () => {
    const rows = buildRows(channels, pricing)
    const all = Object.values(rows).flat()

    expect(all.some((row) => row.model === 'gpt-5' || row.model === 'deepseek-v4')).toBe(false)
    expect(all.some((row) => row.channel.id === 4)).toBe(false)
  })

  it('reports the configured price, or none when the model has no pricing entry', () => {
    const rows = buildRows(channels, pricing)

    expect(rows.image[0].price).toEqual({ kind: 'per-call', usd: 0.04 })
    expect(rows.speech[0].price).toEqual({ kind: 'ratio', ratio: 7.5 })
    expect(rows.transcription[0].price).toBeNull()
  })
})
