import { useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { Link } from 'react-router'
import { Area, AreaChart, CartesianGrid, XAxis, YAxis } from 'recharts'
import {
  ActivityIcon,
  BotIcon,
  RocketIcon,
  SmartphoneIcon,
  SparklesIcon,
  UserPlusIcon,
} from 'lucide-react'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import {
  Card,
  CardAction,
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
import { Progress } from '@/components/ui/progress'
import { Separator } from '@/components/ui/separator'
import { Skeleton } from '@/components/ui/skeleton'
import { ToggleGroup, ToggleGroupItem } from '@/components/ui/toggle-group'
import { PageHeader } from '@/components/page-header'
import { StatCard } from '@/components/stat-card'
import { api, errorMessage } from '@/lib/api'
import { changeRate, compactNumber, formatDate, formatNumber } from '@/lib/format'
import type { ActiveAI, DistributionItem, Overview, TrendPoint } from '@/lib/types'

type Metric = 'dau' | 'launches' | 'new_devices' | 'ai_tokens'

const metricLabels: Record<Metric, string> = {
  dau: '活跃设备',
  launches: '启动次数',
  new_devices: '新增设备',
  ai_tokens: 'AI Token',
}

const chartConfig = {
  value: { label: '数值', color: 'var(--chart-2)' },
} satisfies ChartConfig

export function DashboardPage() {
  const [metric, setMetric] = useState<Metric>('dau')

  const overview = useQuery({
    queryKey: ['stats', 'overview'],
    queryFn: () => api<Overview>('/admin/stats/overview'),
    refetchInterval: 60_000,
  })

  const trend = useQuery({
    queryKey: ['stats', 'trend', 30],
    queryFn: () => api<{ items: TrendPoint[] }>('/admin/stats/trend?days=30'),
  })

  const distribution = useQuery({
    queryKey: ['stats', 'distribution', 'app_version', 7],
    queryFn: () =>
      api<{ items: DistributionItem[] }>('/admin/stats/distribution?dim=app_version&days=7'),
  })

  const active = useQuery({
    queryKey: ['ai', 'active'],
    queryFn: () => api<{ active: ActiveAI | null }>('/admin/ai/active'),
  })

  const data = overview.data
  const series = (trend.data?.items ?? []).map((point) => ({
    date: point.date,
    value: point[metric],
  }))

  return (
    <>
      <PageHeader
        title="仪表盘"
        description="设备活跃、启动与 AI 用量的实时概览，统计按 Asia/Shanghai 切日。"
        actions={
          <Button
            variant="outline"
            size="sm"
            onClick={() => {
              void overview.refetch()
              void trend.refetch()
            }}
          >
            刷新
          </Button>
        }
      />

      {overview.isError ? (
        <Card>
          <CardContent className="py-6 text-sm text-muted-foreground">
            统计加载失败：{errorMessage(overview.error)}
          </CardContent>
        </Card>
      ) : null}

      <div className="grid gap-4 sm:grid-cols-2 xl:grid-cols-4">
        {overview.isLoading || !data ? (
          Array.from({ length: 4 }).map((_, index) => (
            <Skeleton key={index} className="h-28 w-full" />
          ))
        ) : (
          <>
            <StatCard
              label="今日活跃设备"
              value={formatNumber(data.today.dau)}
              icon={SmartphoneIcon}
              delta={changeRate(data.today.dau, data.yesterday.dau)}
              hint={`昨日 ${formatNumber(data.yesterday.dau)}`}
            />
            <StatCard
              label="今日启动次数"
              value={formatNumber(data.today.launches)}
              icon={RocketIcon}
              delta={changeRate(data.today.launches, data.yesterday.launches)}
              hint={`昨日 ${formatNumber(data.yesterday.launches)}`}
            />
            <StatCard
              label="今日新增设备"
              value={formatNumber(data.today.new_devices)}
              icon={UserPlusIcon}
              delta={changeRate(data.today.new_devices, data.yesterday.new_devices)}
              hint={`昨日 ${formatNumber(data.yesterday.new_devices)}`}
            />
            <StatCard
              label="今日 AI Token"
              value={compactNumber(data.today.ai_tokens)}
              icon={SparklesIcon}
              delta={changeRate(data.today.ai_tokens, data.yesterday.ai_tokens)}
              hint={`${formatNumber(data.today.ai_requests)} 次请求`}
            />
          </>
        )}
      </div>

      <div className="grid gap-4 lg:grid-cols-3">
        <Card className="lg:col-span-2">
          <CardHeader>
            <CardTitle>近 30 天趋势</CardTitle>
            <CardDescription>{metricLabels[metric]}按日汇总</CardDescription>
          </CardHeader>
          <CardContent className="flex flex-col gap-3">
            <div className="-mt-1 flex flex-wrap gap-2">
              <ToggleGroup
                variant="outline"
                size="sm"
                value={[metric]}
                onValueChange={(value) => {
                  const next = value[0] as Metric | undefined
                  if (next) setMetric(next)
                }}
              >
                {(Object.keys(metricLabels) as Metric[]).map((key) => (
                  <ToggleGroupItem key={key} value={key}>
                    {metricLabels[key]}
                  </ToggleGroupItem>
                ))}
              </ToggleGroup>
            </div>
            {trend.isLoading ? (
              <Skeleton className="h-64 w-full" />
            ) : series.length === 0 ? (
              <Empty>
                <EmptyHeader>
                  <EmptyMedia variant="icon">
                    <ActivityIcon />
                  </EmptyMedia>
                  <EmptyTitle>暂无数据</EmptyTitle>
                  <EmptyDescription>客户端上报启动后，这里会出现趋势曲线。</EmptyDescription>
                </EmptyHeader>
              </Empty>
            ) : (
              <ChartContainer config={chartConfig} className="h-64 w-full">
                <AreaChart data={series} margin={{ left: 4, right: 8, top: 8 }}>
                  <defs>
                    <linearGradient id="fillMetric" x1="0" y1="0" x2="0" y2="1">
                      <stop offset="0%" stopColor="var(--color-value)" stopOpacity={0.35} />
                      <stop offset="100%" stopColor="var(--color-value)" stopOpacity={0.02} />
                    </linearGradient>
                  </defs>
                  <CartesianGrid vertical={false} strokeDasharray="3 3" />
                  <XAxis
                    dataKey="date"
                    tickLine={false}
                    axisLine={false}
                    tickMargin={8}
                    minTickGap={24}
                    tickFormatter={(value: string) => formatDate(value)}
                  />
                  <YAxis
                    tickLine={false}
                    axisLine={false}
                    width={56}
                    tickFormatter={(value: number) => compactNumber(value)}
                  />
                  <ChartTooltip
                    content={
                      <ChartTooltipContent
                        labelFormatter={(label) => `${label}`}
                        formatter={(value) => formatNumber(Number(value))}
                      />
                    }
                  />
                  <Area
                    dataKey="value"
                    type="monotone"
                    stroke="var(--color-value)"
                    strokeWidth={2}
                    fill="url(#fillMetric)"
                  />
                </AreaChart>
              </ChartContainer>
            )}
          </CardContent>
        </Card>

        <div className="flex flex-col gap-4">
          <Card>
            <CardHeader>
              <CardTitle>AI 模型</CardTitle>
              <CardDescription>默认模型，客户端不指定时使用</CardDescription>
              <CardAction>
                <Button variant="ghost" size="xs" nativeButton={false} render={<Link to="/ai/providers" />}>
                  管理
                </Button>
              </CardAction>
            </CardHeader>
            <CardContent className="flex flex-col gap-3">
              {active.isLoading ? (
                <Skeleton className="h-16 w-full" />
              ) : active.data?.active ? (
                <>
                  <div className="flex items-center gap-2">
                    <div className="flex size-8 items-center justify-center rounded-md bg-muted">
                      <BotIcon className="size-4" aria-hidden />
                    </div>
                    <div className="flex flex-col">
                      <span className="text-sm font-medium">{active.data.active.display_name}</span>
                      <span className="text-xs text-muted-foreground">
                        {active.data.active.provider_name} · {active.data.active.alias}
                      </span>
                    </div>
                    <Badge variant="secondary" className="ml-auto">
                      默认
                    </Badge>
                  </div>
                  <Separator />
                  <div className="flex items-center justify-between text-xs text-muted-foreground">
                    <span>上下文长度</span>
                    <span className="tabular-nums">
                      {active.data.active.context_window
                        ? formatNumber(active.data.active.context_window)
                        : '未知'}
                    </span>
                  </div>
                </>
              ) : (
                <div className="flex flex-col gap-2">
                  <p className="text-sm text-muted-foreground">
                    尚未启用任何模型，客户端会隐藏 AI 入口。
                  </p>
                  <Button size="sm" nativeButton={false} render={<Link to="/ai/providers" />}>
                    去配置供应商
                  </Button>
                </div>
              )}
            </CardContent>
          </Card>

          <Card>
            <CardHeader>
              <CardTitle>活跃概览</CardTitle>
              <CardDescription>跨日去重口径</CardDescription>
            </CardHeader>
            <CardContent className="flex flex-col gap-3">
              <div className="flex items-center justify-between">
                <span className="text-sm text-muted-foreground">周活跃（WAU）</span>
                <span className="font-heading text-lg tabular-nums">
                  {formatNumber(data?.wau)}
                </span>
              </div>
              <div className="flex items-center justify-between">
                <span className="text-sm text-muted-foreground">月活跃（MAU）</span>
                <span className="font-heading text-lg tabular-nums">
                  {formatNumber(data?.mau)}
                </span>
              </div>
              <div className="flex items-center justify-between">
                <span className="text-sm text-muted-foreground">统计日期</span>
                <span className="text-sm tabular-nums">{data?.date ?? '—'}</span>
              </div>
            </CardContent>
          </Card>
        </div>
      </div>

      <Card>
        <CardHeader>
          <CardTitle>App 版本分布</CardTitle>
          <CardDescription>近 7 天活跃设备按版本聚合</CardDescription>
        </CardHeader>
        <CardContent className="flex flex-col gap-3">
          {distribution.isLoading ? (
            <Skeleton className="h-24 w-full" />
          ) : (distribution.data?.items.length ?? 0) === 0 ? (
            <p className="text-sm text-muted-foreground">暂无版本数据。</p>
          ) : (
            distribution.data?.items.map((item) => {
              const total = distribution.data.items.reduce((sum, row) => sum + row.count, 0)
              const percent = total > 0 ? Math.round((item.count / total) * 100) : 0
              return (
                <div key={item.key} className="flex flex-col gap-1">
                  <div className="flex items-center justify-between text-sm">
                    <span className="font-medium">{item.key}</span>
                    <span className="text-muted-foreground tabular-nums">
                      {formatNumber(item.count)} · {percent}%
                    </span>
                  </div>
                  <Progress value={percent} className="h-1.5" />
                </div>
              )
            })
          )}
        </CardContent>
      </Card>
    </>
  )
}
