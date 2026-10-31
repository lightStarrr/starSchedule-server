import { useMemo, useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { Bar, BarChart, CartesianGrid, XAxis, YAxis } from 'recharts'
import { BarChart3Icon } from 'lucide-react'
import { Badge } from '@/components/ui/badge'
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from '@/components/ui/card'
import {
  ChartContainer,
  ChartTooltip,
  ChartTooltipContent,
  type ChartConfig,
} from '@/components/ui/chart'
import { Empty, EmptyDescription, EmptyHeader, EmptyMedia, EmptyTitle } from '@/components/ui/empty'
import { Skeleton } from '@/components/ui/skeleton'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import { ToggleGroup, ToggleGroupItem } from '@/components/ui/toggle-group'
import { PageHeader } from '@/components/page-header'
import { StatCard } from '@/components/stat-card'
import { api, errorMessage } from '@/lib/api'
import { compactNumber, formatDate, formatNumber } from '@/lib/format'
import type { UsageByDay, UsageByDevice } from '@/lib/types'

type GroupBy = 'day' | 'device'

const chartConfig = {
  tokens: { label: 'Token', color: 'var(--chart-2)' },
} satisfies ChartConfig

function isoDate(date: Date): string {
  const year = date.getFullYear()
  const month = `${date.getMonth() + 1}`.padStart(2, '0')
  const day = `${date.getDate()}`.padStart(2, '0')
  return `${year}-${month}-${day}`
}

export function UsagePage() {
  const [days, setDays] = useState<'7' | '30'>('7')
  const [groupBy, setGroupBy] = useState<GroupBy>('day')

  const range = useMemo(() => {
    const to = new Date()
    const from = new Date()
    from.setDate(to.getDate() - (Number(days) - 1))
    return { from: isoDate(from), to: isoDate(to) }
  }, [days])

  const usage = useQuery({
    queryKey: ['ai', 'usage', range.from, range.to, groupBy],
    queryFn: () =>
      api<{ group_by: GroupBy; items: UsageByDay[] | UsageByDevice[] }>(
        `/admin/ai/usage?from=${range.from}&to=${range.to}&group_by=${groupBy}`,
      ),
  })

  const dayItems = groupBy === 'day' ? (usage.data?.items as UsageByDay[] | undefined) ?? [] : []
  const deviceItems =
    groupBy === 'device' ? (usage.data?.items as UsageByDevice[] | undefined) ?? [] : []

  const totals = dayItems.reduce(
    (acc, item) => ({
      tokens: acc.tokens + item.tokens,
      requests: acc.requests + item.request_count,
      errors: acc.errors + item.errors,
    }),
    { tokens: 0, requests: 0, errors: 0 },
  )

  return (
    <>
      <PageHeader
        title="AI 用量"
        description="按设备维度记账，token 以上游返回的 usage 为准，缺失时用估算值兜底。"
        actions={
          <div className="flex items-center gap-2">
            <ToggleGroup
              variant="outline"
              size="sm"
              value={[days]}
              onValueChange={(value) => {
                const next = value[0] as '7' | '30' | undefined
                if (next) setDays(next)
              }}
            >
              <ToggleGroupItem value="7">近 7 天</ToggleGroupItem>
              <ToggleGroupItem value="30">近 30 天</ToggleGroupItem>
            </ToggleGroup>
            <ToggleGroup
              variant="outline"
              size="sm"
              value={[groupBy]}
              onValueChange={(value) => {
                const next = value[0] as GroupBy | undefined
                if (next) setGroupBy(next)
              }}
            >
              <ToggleGroupItem value="day">按天</ToggleGroupItem>
              <ToggleGroupItem value="device">按设备</ToggleGroupItem>
            </ToggleGroup>
          </div>
        }
      />

      {groupBy === 'day' ? (
        <div className="grid gap-4 sm:grid-cols-3">
          <StatCard
            label="区间 Token"
            value={compactNumber(totals.tokens)}
            icon={BarChart3Icon}
            hint={`${formatNumber(totals.requests)} 次请求`}
          />
          <StatCard
            label="失败请求"
            value={formatNumber(totals.errors)}
            icon={BarChart3Icon}
            hint="含上游错误与超时"
          />
          <StatCard
            label="平均每次 Token"
            value={
              totals.requests > 0 ? formatNumber(Math.round(totals.tokens / totals.requests)) : '—'
            }
            icon={BarChart3Icon}
            hint="用于估算单次成本"
          />
        </div>
      ) : null}

      <Card>
        <CardHeader>
          <CardTitle>{groupBy === 'day' ? '每日 Token 用量' : '设备用量 TOP 20'}</CardTitle>
          <CardDescription>
            {range.from} 至 {range.to}
          </CardDescription>
        </CardHeader>
        <CardContent>
          {usage.isLoading ? (
            <Skeleton className="h-56 w-full" />
          ) : usage.isError ? (
            <p className="text-sm text-muted-foreground">加载失败：{errorMessage(usage.error)}</p>
          ) : groupBy === 'day' ? (
            dayItems.length === 0 ? (
              <Empty>
                <EmptyHeader>
                  <EmptyMedia variant="icon">
                    <BarChart3Icon />
                  </EmptyMedia>
                  <EmptyTitle>区间内没有调用</EmptyTitle>
                  <EmptyDescription>客户端使用 AI 后，这里会出现用量曲线。</EmptyDescription>
                </EmptyHeader>
              </Empty>
            ) : (
              <ChartContainer config={chartConfig} className="h-64 w-full">
                <BarChart data={dayItems} margin={{ left: 4, right: 8, top: 8 }}>
                  <CartesianGrid vertical={false} strokeDasharray="3 3" />
                  <XAxis
                    dataKey="date"
                    tickLine={false}
                    axisLine={false}
                    tickMargin={8}
                    minTickGap={16}
                    tickFormatter={(value: string) => formatDate(value)}
                  />
                  <YAxis
                    tickLine={false}
                    axisLine={false}
                    width={56}
                    tickFormatter={(value: number) => compactNumber(value)}
                  />
                  <ChartTooltip content={<ChartTooltipContent />} />
                  <Bar dataKey="tokens" fill="var(--color-tokens)" radius={4} />
                </BarChart>
              </ChartContainer>
            )
          ) : deviceItems.length === 0 ? (
            <Empty>
              <EmptyHeader>
                <EmptyMedia variant="icon">
                  <BarChart3Icon />
                </EmptyMedia>
                <EmptyTitle>区间内没有设备调用</EmptyTitle>
                <EmptyDescription>换个时间范围看看。</EmptyDescription>
              </EmptyHeader>
            </Empty>
          ) : (
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>设备</TableHead>
                  <TableHead className="text-right">请求</TableHead>
                  <TableHead className="text-right">Token</TableHead>
                  <TableHead className="text-right">失败</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {deviceItems.map((item) => (
                  <TableRow key={item.device_id}>
                    <TableCell>
                      <div className="flex flex-col gap-0.5">
                        <span className="font-medium">{item.install_id.slice(0, 8) || '已删除设备'}…</span>
                        <span className="font-mono text-xs text-muted-foreground">
                          {item.device_id.slice(0, 8)}…
                        </span>
                      </div>
                    </TableCell>
                    <TableCell className="text-right tabular-nums">
                      {formatNumber(item.request_count)}
                    </TableCell>
                    <TableCell className="text-right tabular-nums">
                      {formatNumber(item.tokens)}
                    </TableCell>
                    <TableCell className="text-right">
                      {item.errors > 0 ? (
                        <Badge variant="destructive">{formatNumber(item.errors)}</Badge>
                      ) : (
                        <span className="text-muted-foreground">0</span>
                      )}
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          )}
        </CardContent>
      </Card>
    </>
  )
}
