import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Link, useParams } from 'react-router'
import { Bar, BarChart, CartesianGrid, XAxis, YAxis } from 'recharts'
import { ArrowLeftIcon, BanIcon, ShieldCheckIcon } from 'lucide-react'
import { toast } from 'sonner'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
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
import { Separator } from '@/components/ui/separator'
import { Skeleton } from '@/components/ui/skeleton'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import { PageHeader } from '@/components/page-header'
import { api, errorMessage } from '@/lib/api'
import { formatDate, formatDateTime, formatNumber } from '@/lib/format'
import type { DeviceDetail } from '@/lib/types'

const activityConfig = {
  launch_count: { label: '启动次数', color: 'var(--chart-2)' },
} satisfies ChartConfig

function InfoRow({ label, value }: { label: string; value: string }) {
  return (
    <div className="flex items-center justify-between gap-4 py-1.5 text-sm">
      <span className="text-muted-foreground">{label}</span>
      <span className="truncate font-medium tabular-nums">{value}</span>
    </div>
  )
}

export function DeviceDetailPage() {
  const { id } = useParams<{ id: string }>()
  const queryClient = useQueryClient()

  const detail = useQuery({
    queryKey: ['device', id],
    queryFn: () => api<DeviceDetail>(`/admin/devices/${id}`),
    enabled: Boolean(id),
  })

  const toggleBan = useMutation({
    mutationFn: (banned: boolean) =>
      api(`/admin/devices/${id}/ban`, {
        method: 'POST',
        body: { banned, reason: banned ? '后台手动封禁' : '' },
      }),
    onSuccess: (_data, banned) => {
      toast.success(banned ? '设备已封禁' : '设备已解封')
      void queryClient.invalidateQueries({ queryKey: ['device', id] })
      void queryClient.invalidateQueries({ queryKey: ['devices'] })
    },
    onError: (error) => toast.error(errorMessage(error)),
  })

  if (detail.isLoading) {
    return (
      <div className="flex flex-col gap-4">
        <Skeleton className="h-8 w-64" />
        <Skeleton className="h-40 w-full" />
        <Skeleton className="h-64 w-full" />
      </div>
    )
  }

  if (detail.isError || !detail.data) {
    return (
      <Card>
        <CardContent className="flex flex-col items-start gap-3 py-6">
          <p className="text-sm text-muted-foreground">
            加载失败：{detail.error ? errorMessage(detail.error) : '设备不存在'}
          </p>
          <Button variant="outline" size="sm" render={<Link to="/devices" />}>
            返回设备列表
          </Button>
        </CardContent>
      </Card>
    )
  }

  const { device, activity, launches, usage_today } = detail.data
  const chartData = activity.map((item) => ({
    date: item.stat_date,
    launch_count: item.launch_count,
  }))

  return (
    <>
      <PageHeader
        title={`设备 ${device.install_id.slice(0, 8)}…`}
        description={device.id}
        actions={
          <>
            <Button variant="outline" size="sm" render={<Link to="/devices" />}>
              <ArrowLeftIcon data-icon="inline-start" />
              返回
            </Button>
            <Button
              variant={device.status === 'banned' ? 'default' : 'destructive'}
              size="sm"
              disabled={toggleBan.isPending}
              onClick={() => toggleBan.mutate(device.status !== 'banned')}
            >
              {device.status === 'banned' ? (
                <ShieldCheckIcon data-icon="inline-start" />
              ) : (
                <BanIcon data-icon="inline-start" />
              )}
              {device.status === 'banned' ? '解封' : '封禁'}
            </Button>
          </>
        }
      />

      <div className="grid gap-4 lg:grid-cols-3">
        <Card>
          <CardHeader>
            <CardTitle>基本信息</CardTitle>
            <CardDescription>首次注册与最近活跃</CardDescription>
          </CardHeader>
          <CardContent className="flex flex-col">
            <InfoRow label="状态" value={device.status === 'banned' ? '已封禁' : '正常'} />
            {device.ban_reason ? <InfoRow label="封禁原因" value={device.ban_reason} /> : null}
            <Separator className="my-2" />
            <InfoRow label="Android" value={`${device.os_version || '—'} / SDK ${device.sdk_int || '—'}`} />
            <InfoRow label="架构" value={device.arch || '—'} />
            <InfoRow label="App 版本" value={device.app_version || '—'} />
            <InfoRow label="最近 IP" value={device.last_ip || '—'} />
            <Separator className="my-2" />
            <InfoRow label="首次注册" value={formatDateTime(device.first_seen_at)} />
            <InfoRow label="最近活跃" value={formatDateTime(device.last_seen_at)} />
            <InfoRow label="累计启动" value={formatNumber(device.launch_count)} />
            <InfoRow label="疑似重装" value={device.suspect_reinstall ? '是' : '否'} />
          </CardContent>
        </Card>

        <Card className="lg:col-span-2">
          <CardHeader>
            <CardTitle>近 30 天活跃</CardTitle>
            <CardDescription>按日启动次数</CardDescription>
          </CardHeader>
          <CardContent>
            {chartData.length === 0 ? (
              <p className="py-8 text-center text-sm text-muted-foreground">暂无启动记录。</p>
            ) : (
              <ChartContainer config={activityConfig} className="h-56 w-full">
                <BarChart data={chartData} margin={{ left: 4, right: 8, top: 8 }}>
                  <CartesianGrid vertical={false} strokeDasharray="3 3" />
                  <XAxis
                    dataKey="date"
                    tickLine={false}
                    axisLine={false}
                    tickMargin={8}
                    minTickGap={16}
                    tickFormatter={(value: string) => formatDate(value)}
                  />
                  <YAxis tickLine={false} axisLine={false} width={40} allowDecimals={false} />
                  <ChartTooltip content={<ChartTooltipContent />} />
                  <Bar dataKey="launch_count" fill="var(--color-launch_count)" radius={4} />
                </BarChart>
              </ChartContainer>
            )}
          </CardContent>
        </Card>
      </div>

      <div className="grid gap-4 md:grid-cols-3">
        <Card size="sm">
          <CardHeader>
            <CardTitle className="text-xs font-medium text-muted-foreground">今日 AI 请求</CardTitle>
          </CardHeader>
          <CardContent>
            <div className="font-heading text-2xl tabular-nums">
              {formatNumber(usage_today.request_count)}
            </div>
          </CardContent>
        </Card>
        <Card size="sm">
          <CardHeader>
            <CardTitle className="text-xs font-medium text-muted-foreground">今日 Token</CardTitle>
          </CardHeader>
          <CardContent>
            <div className="font-heading text-2xl tabular-nums">
              {formatNumber(usage_today.tokens)}
            </div>
          </CardContent>
        </Card>
        <Card size="sm">
          <CardHeader>
            <CardTitle className="text-xs font-medium text-muted-foreground">估算口径次数</CardTitle>
          </CardHeader>
          <CardContent>
            <div className="font-heading text-2xl tabular-nums">
              {formatNumber(usage_today.estimated_count)}
            </div>
          </CardContent>
        </Card>
      </div>

      <Card className="overflow-hidden py-0">
        <CardHeader className="px-4 pt-4">
          <CardTitle>最近启动</CardTitle>
          <CardDescription>最多展示 20 条</CardDescription>
        </CardHeader>
        <CardContent className="p-0">
          {launches.length === 0 ? (
            <p className="p-6 text-sm text-muted-foreground">暂无记录。</p>
          ) : (
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>时间</TableHead>
                  <TableHead>类型</TableHead>
                  <TableHead className="hidden md:table-cell">App 版本</TableHead>
                  <TableHead className="hidden lg:table-cell">Session</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {launches.map((item) => (
                  <TableRow key={item.id}>
                    <TableCell className="tabular-nums">{formatDateTime(item.occurred_at)}</TableCell>
                    <TableCell>
                      <Badge variant="outline">{item.launch_type === 'warm' ? '热启动' : '冷启动'}</Badge>
                    </TableCell>
                    <TableCell className="hidden md:table-cell tabular-nums">
                      {item.app_version || '—'}
                    </TableCell>
                    <TableCell className="hidden lg:table-cell font-mono text-xs text-muted-foreground">
                      {item.session_id.slice(0, 8)}…
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
