const numberFormatter = new Intl.NumberFormat('zh-CN', { maximumFractionDigits: 1 })

export function formatNumber(value: number | undefined | null): string {
  if (value === undefined || value === null || Number.isNaN(value)) return '—'
  return numberFormatter.format(value)
}

/** 大数字压缩为 1.2万 / 3.4亿，便于指标卡展示。 */
export function compactNumber(value: number | undefined | null): string {
  if (value === undefined || value === null || Number.isNaN(value)) return '—'
  const abs = Math.abs(value)
  if (abs >= 100_000_000) return `${(value / 100_000_000).toFixed(1)}亿`
  if (abs >= 10_000) return `${(value / 10_000).toFixed(1)}万`
  return numberFormatter.format(value)
}

export function formatDateTime(value?: string): string {
  if (!value) return '—'
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return '—'
  return new Intl.DateTimeFormat('zh-CN', {
    year: 'numeric',
    month: '2-digit',
    day: '2-digit',
    hour: '2-digit',
    minute: '2-digit',
    hour12: false,
  }).format(date)
}

export function formatDate(value?: string): string {
  if (!value) return '—'
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return '—'
  return new Intl.DateTimeFormat('zh-CN', {
    month: '2-digit',
    day: '2-digit',
  }).format(date)
}

/** 环比：昨天为 0 时避免出现 Infinity。 */
export function changeRate(today: number, yesterday: number): number | null {
  if (!yesterday) return today > 0 ? null : 0
  return ((today - yesterday) / yesterday) * 100
}

export function formatRate(rate: number | null): string {
  if (rate === null) return '新增'
  const sign = rate > 0 ? '+' : ''
  return `${sign}${rate.toFixed(1)}%`
}
