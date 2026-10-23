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
import { getChannels } from '@/features/channels/api'
import { fetchTokenKey, getApiKeys } from '@/features/keys/api'
import { getPricing } from '@/features/pricing/api'

import type { GatewayChannel, GatewayPricing } from './lib/rows'

const CHANNEL_PAGE = 100
/** Enough for any deployment the overview is meant for; the channel list stays the place for more. */
const MAX_CHANNEL_PAGES = 50

/** Every enabled channel, read page by page. */
export async function getEnabledChannels(): Promise<GatewayChannel[]> {
  const channels: GatewayChannel[] = []
  for (let page = 1; page <= MAX_CHANNEL_PAGES; page++) {
    const res = await getChannels({ p: page, page_size: CHANNEL_PAGE, status: 'enabled' })
    if (!res.success) throw new Error(res.message || 'Failed to load channels')
    const items = res.data?.items ?? []
    channels.push(...items.map((item) => ({ id: item.id, name: item.name, status: item.status, models: item.models })))
    if (items.length < CHANNEL_PAGE || channels.length >= (res.data?.total ?? 0)) break
  }
  return channels
}

export async function getModelPricing(): Promise<GatewayPricing[]> {
  const res = await getPricing()
  if (!res.success) throw new Error(res.message || 'Failed to load pricing')
  return res.data ?? []
}

export type TokenOption = { id: number; name: string }

/** The current user's enabled tokens, to pick one for tests. */
export async function getTokenOptions(): Promise<TokenOption[]> {
  const res = await getApiKeys({ p: 1, size: 100 })
  if (!res.success) throw new Error(res.message || 'Failed to load tokens')
  return (res.data?.items ?? []).filter((token) => token.status === 1).map((token) => ({ id: token.id, name: token.name }))
}

/** The token's real key, without the `sk-` prefix. */
export async function getTokenKey(id: number): Promise<string> {
  const res = await fetchTokenKey(id)
  if (!res.success || !res.data?.key) throw new Error(res.message || 'Failed to read the token key')
  return res.data.key
}
