import { useEffect, useMemo, useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Link } from 'react-router'
import { BanIcon, RotateCwIcon, SearchIcon, ShieldCheckIcon, SmartphoneIcon } from 'lucide-react'
import { toast } from 'sonner'
import {
  AlertDialog,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from '@/components/ui/alert-dialog'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card, CardContent } from '@/components/ui/card'
import { Empty, EmptyDescription, EmptyHeader, EmptyMedia, EmptyTitle } from '@/components/ui/empty'
import {
  InputGroup,
  InputGroupAddon,
  InputGroupInput,
} from '@/components/ui/input-group'
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
import { api, errorMessage } from '@/lib/api'
import { formatDateTime, formatNumber } from '@/lib/format'
import type { CursorPage, DeviceSummary } from '@/lib/types'

type StatusFilter = 'all' | 'active' | 'banned'

export function DevicesPage() {
  const queryClient = useQueryClient()
  const [rawQuery, setRawQuery] = useState('')
  const [query, setQuery] = useState('')
  const [status, setStatus] = useState<StatusFilter>('all')
  const [page, setPage] = useState(0)
  const [cursors, setCursors] = useState<string[]>([''])
  const [target, setTarget] = useState<DeviceSummary | null>(null)

  useEffect(() => {
    const timer = window.setTimeout(() => {
      setQuery(rawQuery.trim())
      setPage(0)
      setCursors([''])
    }, 300)
    return () => window.clearTimeout(timer)
  }, [rawQuery])

  const params = useMemo(() => {
    const search = new URLSearchParams({ limit: '20' })
    if (query) search.set('q', query)
    if (status !== 'all') search.set('status', status)
    if (cursors[page]) search.set('cursor', cursors[page])
    return search.toString()
  }, [query, status, page, cursors])

  const devices = useQuery({
    queryKey: ['devices', params],
    queryFn: () => api<CursorPage<DeviceSummary>>(`/admin/devices?${params}`),
  })

  const toggleBan = useMutation({
    mutationFn: (device: DeviceSummary) =>
      api<{ device: DeviceSummary }>(`/admin/devices/${device.id}/ban`, {
        method: 'POST',
        body: {
          banned: device.status !== 'banned',
          reason: device.status === 'banned' ? '' : '后台手动封禁',
        },
      }),
    onSuccess: (result) => {
      toast.success(result.device.status === 'banned' ? '设备已封禁' : '设备已解封')
      setTarget(null)
      void queryClient.invalidateQueries({ queryKey: ['devices'] })
    },
    onError: (error) => toast.error(errorMessage(error)),
  })

  const items = devices.data?.items ?? []

  return (
    <>
      <PageHeader
        title="设备"
        description="按 install_id 去重统计；封禁后该设备的启动上报与 AI 请求会立即被拒绝。"
        actions={
          <Button
            variant="outline"
            size="sm"
            onClick={() => void devices.refetch()}
            disabled={devices.isFetching}
          >
            <RotateCwIcon data-icon="inline-start" />
            刷新
          </Button>
        }
      />

      <div className="flex flex-wrap items-center gap-2">
        <InputGroup className="w-full sm:w-80">
          <InputGroupAddon>
            <SearchIcon />
          </InputGroupAddon>
          <InputGroupInput
            placeholder="搜索 install_id 或 设备 ID"
            value={rawQuery}
            onChange={(event) => setRawQuery(event.target.value)}
          />
        </InputGroup>

        <ToggleGroup
          variant="outline"
          size="sm"
          value={[status]}
          onValueChange={(value) => {
            const next = value[0] as StatusFilter | undefined
            if (next) {
              setStatus(next)
              setPage(0)
              setCursors([''])
            }
          }}
        >
          <ToggleGroupItem value="all">全部</ToggleGroupItem>
          <ToggleGroupItem value="active">正常</ToggleGroupItem>
          <ToggleGroupItem value="banned">已封禁</ToggleGroupItem>
        </ToggleGroup>

        <Badge variant="secondary" className="ml-auto tabular-nums">
          约 {formatNumber(devices.data?.total_estimate ?? 0)} 台
        </Badge>
      </div>

      <Card className="overflow-hidden py-0">
        <CardContent className="p-0">
          {devices.isLoading ? (
            <div className="flex flex-col gap-2 p-4">
              {Array.from({ length: 6 }).map((_, index) => (
                <Skeleton key={index} className="h-10 w-full" />
              ))}
            </div>
          ) : devices.isError ? (
            <div className="p-6 text-sm text-muted-foreground">
              加载失败：{errorMessage(devices.error)}
            </div>
          ) : items.length === 0 ? (
            <Empty className="border-0">
              <EmptyHeader>
                <EmptyMedia variant="icon">
                  <SmartphoneIcon />
                </EmptyMedia>
                <EmptyTitle>没有匹配的设备</EmptyTitle>
                <EmptyDescription>
                  客户端首次启动注册后，设备会出现在这里。可尝试清空搜索条件。
                </EmptyDescription>
              </EmptyHeader>
            </Empty>
          ) : (
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>设备</TableHead>
                  <TableHead className="hidden md:table-cell">Android</TableHead>
                  <TableHead className="hidden lg:table-cell">App 版本</TableHead>
                  <TableHead className="text-right">启动次数</TableHead>
                  <TableHead className="hidden md:table-cell">最近活跃</TableHead>
                  <TableHead className="text-right">操作</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {items.map((device) => (
                  <TableRow key={device.id}>
                    <TableCell>
                      <div className="flex flex-col gap-0.5">
                        <Link
                          to={`/devices/${device.id}`}
                          className="font-medium hover:underline"
                        >
                          {device.install_id.slice(0, 8)}…
                        </Link>
                        <span className="text-xs text-muted-foreground">
                          {device.id.slice(0, 8)} · {device.arch || '—'}
                          {device.suspect_reinstall ? ' · 疑似重装' : ''}
                        </span>
                      </div>
                    </TableCell>
                    <TableCell className="hidden md:table-cell">
                      <div className="flex items-center gap-2">
                        <span className="text-sm">Android {device.os_version || '—'}</span>
                        <Badge variant="outline">SDK {device.sdk_int || '—'}</Badge>
                      </div>
                    </TableCell>
                    <TableCell className="hidden lg:table-cell tabular-nums">
                      {device.app_version || '—'}
                    </TableCell>
                    <TableCell className="text-right tabular-nums">
                      {formatNumber(device.launch_count)}
                    </TableCell>
                    <TableCell className="hidden md:table-cell text-sm text-muted-foreground">
                      {formatDateTime(device.last_seen_at)}
                    </TableCell>
                    <TableCell className="text-right">
                      <div className="flex items-center justify-end gap-2">
                        <Badge variant={device.status === 'banned' ? 'destructive' : 'secondary'}>
                          {device.status === 'banned' ? '已封禁' : '正常'}
                        </Badge>
                        <Button
                          variant="ghost"
                          size="icon-sm"
                          aria-label={device.status === 'banned' ? '解封' : '封禁'}
                          onClick={() => setTarget(device)}
                        >
                          {device.status === 'banned' ? <ShieldCheckIcon /> : <BanIcon />}
                        </Button>
                      </div>
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          )}
        </CardContent>
      </Card>

      <div className="flex items-center justify-between">
        <span className="text-xs text-muted-foreground">
          第 {page + 1} 页 · 每页 20 条
        </span>
        <div className="flex items-center gap-2">
          <Button
            variant="outline"
            size="sm"
            disabled={page === 0}
            onClick={() => setPage((value) => Math.max(0, value - 1))}
          >
            上一页
          </Button>
          <Button
            variant="outline"
            size="sm"
            disabled={!devices.data?.has_more}
            onClick={() => {
              const next = devices.data?.next_cursor
              if (!next) return
              setCursors((value) => {
                const copy = value.slice(0, page + 1)
                copy.push(next)
                return copy
              })
              setPage((value) => value + 1)
            }}
          >
            下一页
          </Button>
        </div>
      </div>

      <AlertDialog open={target !== null} onOpenChange={(open) => !open && setTarget(null)}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>
              {target?.status === 'banned' ? '解封该设备？' : '封禁该设备？'}
            </AlertDialogTitle>
            <AlertDialogDescription>
              {target?.status === 'banned'
                ? '解封后客户端可继续上报启动并调用 AI。'
                : '封禁后该设备的 token 立即失效，启动上报与 AI 请求都会被拒绝。'}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>取消</AlertDialogCancel>
            <Button
              variant={target?.status === 'banned' ? 'default' : 'destructive'}
              disabled={toggleBan.isPending}
              onClick={() => target && toggleBan.mutate(target)}
            >
              {toggleBan.isPending ? '处理中…' : target?.status === 'banned' ? '解封' : '确认封禁'}
            </Button>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </>
  )
}
