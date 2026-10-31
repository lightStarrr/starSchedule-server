import type { LucideIcon } from 'lucide-react'
import { ArrowDownRightIcon, ArrowUpRightIcon } from 'lucide-react'
import { Badge } from '@/components/ui/badge'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { formatRate } from '@/lib/format'

interface StatCardProps {
  label: string
  value: string
  icon: LucideIcon
  delta?: number | null
  hint?: string
}

export function StatCard({ label, value, icon: Icon, delta, hint }: StatCardProps) {
  const hasDelta = delta !== undefined
  const up = typeof delta === 'number' && delta > 0
  const down = typeof delta === 'number' && delta < 0

  return (
    <Card size="sm">
      <CardHeader>
        <CardTitle className="flex items-center justify-between text-xs font-medium text-muted-foreground">
          <span>{label}</span>
          <Icon className="size-3.5 text-muted-foreground/70" aria-hidden />
        </CardTitle>
      </CardHeader>
      <CardContent className="flex flex-col gap-2">
        <div className="font-heading text-2xl font-semibold tabular-nums tracking-tight">{value}</div>
        <div className="flex items-center gap-2 text-xs text-muted-foreground">
          {hasDelta ? (
            delta === null ? (
              <Badge variant="secondary">新增</Badge>
            ) : (
              <Badge variant="secondary" className="gap-0.5 tabular-nums">
                {up ? <ArrowUpRightIcon /> : null}
                {down ? <ArrowDownRightIcon /> : null}
                {formatRate(delta ?? 0)}
              </Badge>
            )
          ) : null}
          {hint ? <span className="truncate">{hint}</span> : null}
        </div>
      </CardContent>
    </Card>
  )
}
