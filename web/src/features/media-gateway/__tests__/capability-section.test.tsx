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
import { fireEvent, render, screen } from '@testing-library/react'
import { describe, expect, test, vi } from 'vitest'

import { CapabilitySection } from '../components/capability-section'
import { CAPABILITIES } from '../lib/capabilities'
import { testKey, type GatewayRow } from '../lib/rows'

const speech = CAPABILITIES.find((capability) => capability.id === 'speech')!
const search = CAPABILITIES.find((capability) => capability.id === 'search')!
const rows: GatewayRow[] = [
  { model: 'tts-1', channel: { id: 1, name: 'OpenAI' }, price: { kind: 'ratio', ratio: 7.5 } },
  { model: 'tts-1', channel: { id: 3, name: 'Backup' }, price: null },
]
const channels = [
  { id: 1, name: 'OpenAI', status: 1, models: 'tts-1' },
  { id: 3, name: 'Backup', status: 1, models: 'tts-1' },
]

function renderSection(props: Partial<Parameters<typeof CapabilitySection>[0]> = {}) {
  const onTest = vi.fn()
  render(
    <CapabilitySection
      capability={speech}
      rows={rows}
      channels={channels}
      results={{}}
      canTest
      onTest={onTest}
      {...props}
    />
  )
  return { onTest }
}

describe('CapabilitySection', () => {
  test('tests the channel and model of the row whose Test button is pressed', () => {
    const { onTest } = renderSection()

    fireEvent.click(screen.getAllByRole('button', { name: 'Test' })[1])

    expect(onTest).toHaveBeenCalledWith('tts-1', 3)
  })

  test('flags a model whose price is not set', () => {
    renderSection()

    expect(screen.getByText('No price set')).toBeInTheDocument()
  })

  test('disables tests until a token is picked', () => {
    renderSection({ canTest: false })

    for (const button of screen.getAllByRole('button', { name: 'Test' })) {
      expect(button).toBeDisabled()
    }
  })

  test('shows a failed test with the upstream’s own reason', () => {
    renderSection({
      results: {
        [testKey('speech', 'tts-1', 1)]: {
          running: false,
          outcome: { ok: false, elapsed: 320, detail: 'voice alloy is not supported' },
        },
      },
    })

    expect(screen.getByText('Failed')).toBeInTheDocument()
    expect(screen.getByText('voice alloy is not supported')).toBeInTheDocument()
  })

  test('tells how to enable a capability with a fixed model when no channel serves it', () => {
    renderSection({ capability: search, rows: [] })

    expect(screen.getByText('No channel serves this yet')).toBeInTheDocument()
    expect(screen.getByRole('textbox', { name: 'Model to test' })).toHaveValue('deepseek-search')
  })
})
