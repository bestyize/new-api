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

import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'

import type { TokenOption } from '../api'

type TokenPickerProps = {
  tokens: TokenOption[]
  value: number | null
  disabled?: boolean
  onChange: (id: number) => void
}

/** Which of the admin's own tokens the tests run with; the calls are billed and logged against it. */
export function TokenPicker(props: TokenPickerProps) {
  const { t } = useTranslation()
  const items = props.tokens.map((token) => ({ value: String(token.id), label: token.name }))
  return (
    <Select
      value={props.value == null ? null : String(props.value)}
      disabled={props.disabled || items.length === 0}
      onValueChange={(value) => {
        if (value) props.onChange(Number(value))
      }}
      items={items}
    >
      <SelectTrigger aria-label={t('Token used for tests')} className='bg-background min-w-44'>
        <SelectValue placeholder={items.length ? t('Pick a token for tests') : t('No enabled token')} />
      </SelectTrigger>
      <SelectContent>
        {items.map((item) => (
          <SelectItem key={item.value} value={item.value}>
            {item.label}
          </SelectItem>
        ))}
      </SelectContent>
    </Select>
  )
}
