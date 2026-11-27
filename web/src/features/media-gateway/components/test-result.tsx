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
import { useTranslation } from 'react-i18next'

import { StatusBadge } from '@/components/status-badge'
import { Spinner } from '@/components/ui/spinner'

import type { TestOutcome } from '../lib/run-test'

export type TestState = { running: true } | { running: false; outcome: TestOutcome }

/** The last test of one channel and model: running, passed with what came back, or failed with why. */
export function TestResult(props: { state?: TestState }) {
  const { t } = useTranslation()
  const state = props.state
  if (!state) return <span className='text-muted-foreground text-sm'>{t('Not tested')}</span>
  if (state.running) {
    return (
      <span className='text-muted-foreground inline-flex items-center gap-1.5 text-sm'>
        <Spinner className='size-3.5' />
        {t('Testing...')}
      </span>
    )
  }
  const outcome = state.outcome
  const detail = outcome.translatable ? t(outcome.detail) : outcome.detail
  return (
    <div className='flex min-w-0 flex-col gap-1'>
      <div className='flex min-w-0 items-center gap-2'>
        <StatusBadge variant={outcome.ok ? 'success' : 'danger'} label={outcome.ok ? t('Works') : t('Failed')} />
        <span className='text-muted-foreground shrink-0 text-xs tabular-nums'>{t('{{ms}} ms', { ms: outcome.elapsed })}</span>
        <span className='truncate text-sm' title={detail}>
          {detail}
        </span>
      </div>
      {outcome.preview?.kind === 'audio' && (
        <audio controls src={outcome.preview.url} className='h-8 max-w-64'>
          <track kind='captions' />
        </audio>
      )}
      {outcome.preview?.kind === 'image' && (
        <img src={outcome.preview.url} alt={t('Generated test image')} className='size-16 rounded-md object-cover' />
      )}
      {outcome.preview?.kind === 'text' && (
        <p className='text-muted-foreground line-clamp-2 text-xs'>{outcome.preview.text}</p>
      )}
    </div>
  )
}
