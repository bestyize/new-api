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
import { useQuery } from '@tanstack/react-query'
import { useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { ConfirmDialog } from '@/components/confirm-dialog'
import { ErrorState } from '@/components/error-state'
import { SectionPageLayout } from '@/components/layout'
import { LoadingState } from '@/components/loading-state'
import { handleServerError } from '@/lib/handle-server-error'

import { getEnabledChannels, getModelPricing, getTokenKey, getTokenOptions } from './api'
import { CapabilitySection } from './components/capability-section'
import type { TestState } from './components/test-result'
import { TokenPicker } from './components/token-picker'
import { CAPABILITIES, type CapabilityId } from './lib/capabilities'
import { buildRows, testKey } from './lib/rows'
import { runTest } from './lib/run-test'

const TOKEN_STORAGE_KEY = 'media-gateway.token'

function savedToken(): number | null {
  try {
    const value = Number(localStorage.getItem(TOKEN_STORAGE_KEY))
    return Number.isInteger(value) && value > 0 ? value : null
  } catch {
    return null
  }
}

type PendingTest = { capability: CapabilityId; model: string; channelId: number }

/**
 * 媒体网关: every non-chat capability in one place — how a client calls it,
 * which enabled channel serves which model at what price, and a test that
 * sends a real minimal request through that channel.
 */
export function MediaGateway() {
  const { t } = useTranslation()
  const channelsQuery = useQuery({ queryKey: ['media-gateway', 'channels'], queryFn: getEnabledChannels })
  const pricingQuery = useQuery({ queryKey: ['media-gateway', 'pricing'], queryFn: getModelPricing })
  const tokensQuery = useQuery({ queryKey: ['media-gateway', 'tokens'], queryFn: getTokenOptions })
  const [tokenId, setTokenId] = useState<number | null>(savedToken)
  const [tokenKeys, setTokenKeys] = useState<Record<number, string>>({})
  const [results, setResults] = useState<Record<string, TestState>>({})
  const [confirming, setConfirming] = useState<PendingTest | null>(null)

  const rows = useMemo(
    () => buildRows(channelsQuery.data ?? [], pricingQuery.data ?? []),
    [channelsQuery.data, pricingQuery.data]
  )
  const activeToken = tokensQuery.data?.some((token) => token.id === tokenId) ? tokenId : null

  const pickToken = (id: number) => {
    setTokenId(id)
    try {
      localStorage.setItem(TOKEN_STORAGE_KEY, String(id))
    } catch {
      // Remembering the choice is a convenience only.
    }
  }

  const run = async (test: PendingTest) => {
    if (activeToken == null) {
      toast.error(t('Pick a token for tests first'))
      return
    }
    const key = testKey(test.capability, test.model, test.channelId)
    setResults((current) => ({ ...current, [key]: { running: true } }))
    try {
      const tokenKey = tokenKeys[activeToken] ?? (await getTokenKey(activeToken))
      setTokenKeys((current) => ({ ...current, [activeToken]: tokenKey }))
      const outcome = await runTest(
        { ...test, tokenKey },
        {
          origin: window.location.origin,
          fetch: window.fetch.bind(window),
          WebSocket: window.WebSocket,
          now: () => performance.now(),
          createObjectURL: (blob) => URL.createObjectURL(blob),
        }
      )
      setResults((current) => ({ ...current, [key]: { running: false, outcome } }))
    } catch (error) {
      setResults((current) => {
        const next = { ...current }
        delete next[key]
        return next
      })
      handleServerError(error)
    }
  }

  const requestTest = (test: PendingTest) => {
    // Generating an image costs noticeably more than the other tests.
    if (test.capability === 'image') setConfirming(test)
    else void run(test)
  }

  const loading = channelsQuery.isLoading || pricingQuery.isLoading
  const failed = channelsQuery.error ?? pricingQuery.error

  return (
    <>
      <SectionPageLayout>
        <SectionPageLayout.Title>{t('Media Gateway')}</SectionPageLayout.Title>
        <SectionPageLayout.Actions>
          <TokenPicker tokens={tokensQuery.data ?? []} value={activeToken} disabled={tokensQuery.isLoading} onChange={pickToken} />
        </SectionPageLayout.Actions>
        <SectionPageLayout.Content>
          {loading && <LoadingState />}
          {!loading && failed && (
            <ErrorState
              description={failed instanceof Error ? failed.message : String(failed)}
              onRetry={() => {
                void channelsQuery.refetch()
                void pricingQuery.refetch()
              }}
            />
          )}
          {!loading && !failed && (
            <div className='flex flex-col gap-4'>
              <p className='text-muted-foreground text-sm'>
                {t('Speech, images, realtime voice, search and more: how clients call each, which channels serve them, and a real test call through a chosen channel. Tests are billed and logged against the chosen token.')}
              </p>
              {CAPABILITIES.map((capability) => (
                <CapabilitySection
                  key={capability.id}
                  capability={capability}
                  rows={rows[capability.id]}
                  channels={channelsQuery.data ?? []}
                  results={results}
                  canTest={activeToken != null}
                  onTest={(model, channelId) => requestTest({ capability: capability.id, model, channelId })}
                />
              ))}
            </div>
          )}
        </SectionPageLayout.Content>
      </SectionPageLayout>
      <ConfirmDialog
        open={confirming != null}
        onOpenChange={(open) => {
          if (!open) setConfirming(null)
        }}
        title={t('Generate a test image?')}
        desc={t('This generates one real image through the channel and is billed at the model’s price.')}
        confirmText={t('Generate')}
        handleConfirm={() => {
          if (confirming) void run(confirming)
          setConfirming(null)
        }}
      />
    </>
  )
}
