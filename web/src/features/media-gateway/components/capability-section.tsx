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
import { useState } from 'react'
import { useTranslation } from 'react-i18next'

import { CopyButton } from '@/components/copy-button'
import { StaticDataTable, type StaticDataTableColumn } from '@/components/data-table'
import { EmptyState } from '@/components/empty-state'
import { StatusBadge } from '@/components/status-badge'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { TitledCard } from '@/components/ui/titled-card'
import { formatBillingCurrencyFromUSD } from '@/lib/currency'
import { formatNumber } from '@/lib/format'
import { toIntlLocale } from '@/i18n/languages'

import type { Capability } from '../lib/capabilities'
import { type GatewayChannel, type GatewayRow, testKey } from '../lib/rows'
import { TestResult, type TestState } from './test-result'

type CapabilitySectionProps = {
  capability: Capability
  rows: GatewayRow[]
  channels: GatewayChannel[]
  results: Record<string, TestState>
  canTest: boolean
  onTest: (model: string, channelId: number) => void
}

const PRICE_DIGITS = { digitsLarge: 4, digitsSmall: 6, abbreviate: false }

/** One capability: how to call it, which channel serves which model at what price, and a test for each. */
export function CapabilitySection(props: CapabilitySectionProps) {
  const { t, i18n } = useTranslation()
  const locale = toIntlLocale(i18n.resolvedLanguage || i18n.language)
  const capability = props.capability
  const [channelId, setChannelId] = useState<number | null>(null)
  const [model, setModel] = useState(capability.fixedModel ?? '')
  const example = `${capability.method === 'WebSocket' ? 'GET' : 'POST'} ${capability.path}\n${capability.example(props.rows[0]?.model ?? (capability.fixedModel || 'model-name'))}`
  const channelItems = props.channels.map((channel) => ({ value: String(channel.id), label: `#${channel.id} ${channel.name}` }))

  const columns: StaticDataTableColumn<GatewayRow>[] = [
    { id: 'model', header: t('Model'), cell: (row) => <span className='font-mono text-sm'>{row.model}</span> },
    { id: 'channel', header: t('Channel'), cell: (row) => `#${row.channel.id} ${row.channel.name}` },
    {
      id: 'price',
      header: t('Price'),
      cell: (row) => {
        if (!row.price) return <StatusBadge variant='warning' label={t('No price set')} />
        if (row.price.kind === 'per-call') return t('{{price}} per call', { price: formatBillingCurrencyFromUSD(row.price.usd, PRICE_DIGITS) })
        return t('Ratio {{ratio}}', { ratio: formatNumber(row.price.ratio, locale) })
      },
    },
    {
      id: 'result',
      header: t('Last test'),
      cellClassName: 'max-w-96',
      cell: (row) => <TestResult state={props.results[testKey(capability.id, row.model, row.channel.id)]} />,
    },
    {
      id: 'test',
      header: <span className='sr-only'>{t('Test')}</span>,
      cell: (row) => (
        <Button
          size='sm'
          variant='outline'
          disabled={!capability.testable || !props.canTest || props.results[testKey(capability.id, row.model, row.channel.id)]?.running === true}
          onClick={() => props.onTest(row.model, row.channel.id)}
        >
          {t('Test')}
        </Button>
      ),
    },
  ]

  return (
    <TitledCard
      title={t(capability.title)}
      description={t(capability.description)}
      action={
        <div className='flex items-center gap-1'>
          <StatusBadge variant='neutral' label={`${capability.method} ${capability.path}`} />
          <CopyButton value={example} tooltip={t('Copy request example')} />
        </div>
      }
    >
      <div className='flex flex-col gap-3'>
        {props.rows.length === 0 ? (
          <EmptyState
            bordered
            title={t('No channel serves this yet')}
            description={
              capability.fixedModel
                ? t('Add the model {{model}} to a channel to enable it.', { model: capability.fixedModel })
                : t('Add a model that supports it to a channel, or try one below.')
            }
          />
        ) : (
          <StaticDataTable
            columns={columns}
            data={props.rows}
            getRowKey={(row) => testKey(capability.id, row.model, row.channel.id)}
          />
        )}
        {capability.testable && (
          <form
            className='flex flex-wrap items-center gap-2'
            onSubmit={(event) => {
              event.preventDefault()
              if (channelId != null && model.trim()) props.onTest(model.trim(), channelId)
            }}
          >
            <Select
              value={channelId == null ? null : String(channelId)}
              onValueChange={(value) => {
                if (value) setChannelId(Number(value))
              }}
              items={channelItems}
            >
              <SelectTrigger aria-label={t('Channel to test')} className='min-w-44'>
                <SelectValue placeholder={t('Channel')} />
              </SelectTrigger>
              <SelectContent>
                {channelItems.map((item) => (
                  <SelectItem key={item.value} value={item.value}>
                    {item.label}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
            <Input
              aria-label={t('Model to test')}
              placeholder={t('Model name')}
              value={model}
              disabled={Boolean(capability.fixedModel)}
              onChange={(event) => setModel(event.target.value)}
              className='w-56'
            />
            <Button type='submit' size='sm' variant='secondary' disabled={!props.canTest || channelId == null || !model.trim()}>
              {t('Test another model')}
            </Button>
            {channelId != null && model.trim() && !props.rows.some((row) => row.model === model.trim() && row.channel.id === channelId) && (
              <TestResult state={props.results[testKey(capability.id, model.trim(), channelId)]} />
            )}
          </form>
        )}
      </div>
    </TitledCard>
  )
}
