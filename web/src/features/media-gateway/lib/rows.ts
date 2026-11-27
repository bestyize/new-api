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
import { CAPABILITIES, type CapabilityId, servesCapability } from './capabilities'

export type GatewayChannel = {
  id: number
  name: string
  status: number
  models: string
}

export type GatewayPricing = {
  model_name: string
  quota_type: number
  model_ratio: number
  model_price?: number
  supported_endpoint_types?: string[]
}

/** What the pricing settings say a call costs: a fixed price per call (USD), a token ratio, or nothing. */
export type ModelPrice =
  | { kind: 'per-call'; usd: number }
  | { kind: 'ratio'; ratio: number }
  | null

export type GatewayRow = {
  model: string
  channel: { id: number; name: string }
  price: ModelPrice
}

/** Where one channel-and-model test's result is kept. */
export function testKey(capability: string, model: string, channelId: number) {
  return `${capability}|${channelId}|${model}`
}

/** Channel status 1 is enabled; the others are disabled by hand or by the system. */
const ENABLED = 1

function priceOf(pricing: GatewayPricing | undefined): ModelPrice {
  if (!pricing) return null
  if (pricing.quota_type === 1) return { kind: 'per-call', usd: pricing.model_price ?? 0 }
  return { kind: 'ratio', ratio: pricing.model_ratio }
}

/**
 * Every enabled channel's models, grouped by the capability each one looks
 * like it serves. A model served by two channels gives two rows, so each
 * channel can be tested on its own.
 */
export function buildRows(
  channels: GatewayChannel[],
  pricing: GatewayPricing[]
): Record<CapabilityId, GatewayRow[]> {
  const byModel = new Map(pricing.map((item) => [item.model_name, item]))
  const rows = Object.fromEntries(
    CAPABILITIES.map((capability) => [capability.id, [] as GatewayRow[]])
  ) as Record<CapabilityId, GatewayRow[]>
  for (const channel of channels) {
    if (channel.status !== ENABLED) continue
    const models = new Set(
      channel.models
        .split(',')
        .map((model) => model.trim())
        .filter(Boolean)
    )
    for (const model of models) {
      const info = byModel.get(model)
      for (const capability of CAPABILITIES) {
        if (!servesCapability(capability, model, info?.supported_endpoint_types ?? [])) continue
        rows[capability.id].push({
          model,
          channel: { id: channel.id, name: channel.name },
          price: priceOf(info),
        })
      }
    }
  }
  for (const list of Object.values(rows)) {
    list.sort((a, b) => a.model.localeCompare(b.model) || a.channel.id - b.channel.id)
  }
  return rows
}
