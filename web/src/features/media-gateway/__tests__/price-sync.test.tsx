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
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { cleanup, render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, describe, expect, it, vi } from 'vitest'

import { api } from '@/lib/api'
import { ROLE } from '@/lib/roles'
import { useAuthStore } from '@/stores/auth-store'

import { MediaGateway } from '..'

vi.mock('../api', () => ({
  getEnabledChannels: async () => [],
  getModelPricing: async () => [],
  getTokenOptions: async () => [],
  getTokenKey: async () => 'key',
}))

function renderPage(role: number) {
  useAuthStore.getState().auth.setUser({ id: 1, username: 'root', role })
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  render(
    <QueryClientProvider client={client}>
      <MediaGateway />
    </QueryClientProvider>
  )
}

afterEach(() => {
  cleanup()
  vi.restoreAllMocks()
  useAuthStore.getState().auth.setUser(null)
})

describe('MediaGateway pricing sync', () => {
  it('opens the model pricing sync dialog for a super admin', async () => {
    vi.spyOn(api, 'get').mockResolvedValue({ data: { success: true, data: [] } })
    renderPage(ROLE.SUPER_ADMIN)
    const user = userEvent.setup()

    await user.click(await screen.findByRole('button', { name: 'Sync pricing' }))

    expect(await screen.findByRole('dialog', { name: 'Sync model pricing' })).toBeVisible()
  })

  it('hides pricing sync from an admin who cannot edit prices', async () => {
    renderPage(ROLE.ADMIN)

    expect(await screen.findByText('Media Gateway')).toBeVisible()
    expect(screen.queryByRole('button', { name: 'Sync pricing' })).not.toBeInTheDocument()
  })
})
