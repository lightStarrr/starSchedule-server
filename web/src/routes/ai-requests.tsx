import { useEffect, useMemo, useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { Link, useSearchParams } from 'react-router'
import { RotateCcwIcon, RotateCwIcon, ScrollTextIcon, SearchIcon, XIcon } from 'lucide-react'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card, CardContent } from '@/components/ui/card'
import { Empty, EmptyDescription, EmptyHeader, EmptyMedia, EmptyTitle } from '@/components/ui/empty'
import {
  InputGroup,
  InputGroupAddon,
  InputGroupInput,
} from '@/components/ui/input-group'
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
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
import { Tooltip, TooltipContent, TooltipTrigger } from '@/components/ui/tooltip'
import { PageHeader } from '@/components/page-header'
import { api, errorMessage } from '@/lib/api'
import {
  formatDateTimeSeconds,
  formatDurationMs,
  formatFullDateTime,
  formatNumber,
} from '@/lib/format'
import type { AIRequestLogItem, AIRequestLogPage, ModelInfo } from '@/lib/types'

type StreamFilter = 'all' | 'true' | 'false'
type StatusFilter = 'all' | 'ok' | 'error'
type RangeFilter = 'today' | '7' | '30' | 'all'

function isoDate(date: Date): string {
  const year = date.getFullYear()
  const month = `${date.getMonth() + 1}`.padStart(2, '0')
  const day = `${date.getDate()}`.padStart(2, '0')
  return `${year}-${month}-${day}`
}

function RequestUserCell({ item }: { item: AIRequestLogItem }) {
  if (item.device_id) {
    const label = item.install_id ? `${item.install_id.slice(0, 8)}…` : `${item.device_id.slice(0, 8)}…`
    return (
      <div className="flex flex-col gap-0.5">
        <Link
          to={`/devices/${item.device_id}`}
          className="font-medium text-foreground hover:underline"
        >
          {label}
        </Link>
        <span className="font-mono text-xs text-muted-foreground">
          {item.device_id.slice(0, 8)}
        </span>
      </div>
    )
  }
  return (
    <div className="flex flex-col gap-0.5">
      <div className="flex items-center gap-1.5">
        <Badge variant="outline">试跑</Badge>
        <span className="text-sm font-medium">{item.admin_username || '管理员'}</span>
      </div>
      <span className="text-xs text-muted-foreground">管理后台</span>
    </div>
  )
}

function StreamTpsCell({ item }: { item: AIRequestLogItem }) {
  return (
    <div className="flex flex-col gap-1">
      <div>
        <Badge variant={item.stream ? 'secondary' : 'outline'}>
          {item.stream ? '流式' : '非流式'}
        </Badge>
      </div>
      <span className="text-xs tabular-nums text-muted-foreground">
        {item.stream && item.tps !== undefined && item.tps !== null
          ? `${item.tps.toFixed(1)} t/s`
          : '—'}
      </span>
    </div>
  )
}

function TokenCell({ item }: { item: AIRequestLogItem }) {
  const cacheRate =
    item.cached_tokens > 0 && item.prompt_tokens > 0
      ? Math.min(100, Math.round((item.cached_tokens / item.prompt_tokens) * 100))
      : null
  return (
    <div className="flex flex-col gap-0.5 tabular-nums">
      <div className="flex flex-wrap items-center gap-1 text-sm">
        <span>入 {formatNumber(item.prompt_tokens)}</span>
        <span className="text-muted-foreground">·</span>
        <span>出 {formatNumber(item.completion_tokens)}</span>
        {item.estimated ? (
          <Badge variant="outline" className="px-1 py-0 text-[10px]">
            估算
          </Badge>
        ) : null}
      </div>
      <span className="text-xs text-muted-foreground">
        缓 {formatNumber(item.cached_tokens)}
        {cacheRate !== null ? ` (${cacheRate}%)` : ''}
      </span>
    </div>
  )
}

function LatencyCell({ item }: { item: AIRequestLogItem }) {
  return (
    <div className="flex flex-col gap-0.5 tabular-nums">
      <span className="text-sm">总 {formatDurationMs(item.total_ms)}</span>
      <span className="text-xs text-muted-foreground">
        {item.stream ? `首 ${formatDurationMs(item.first_token_ms)}` : '首 —'}
      </span>
    </div>
  )
}

export function AIRequestsPage() {
  const [searchParams, setSearchParams] = useSearchParams()
  const deviceId = searchParams.get('device_id')?.trim() ?? ''

  const [rawQuery, setRawQuery] = useState('')
  const [query, setQuery] = useState('')
  const [modelFilter, setModelFilter] = useState('__all__')
  const [streamFilter, setStreamFilter] = useState<StreamFilter>('all')
  const [statusFilter, setStatusFilter] = useState<StatusFilter>('all')
  const [rangeFilter, setRangeFilter] = useState<RangeFilter>('7')
  const [page, setPage] = useState(0)
  const [cursors, setCursors] = useState<string[]>([''])

  useEffect(() => {
    const timer = window.setTimeout(() => {
      setQuery(rawQuery.trim())
      setPage(0)
      setCursors([''])
    }, 300)
    return () => window.clearTimeout(timer)
  }, [rawQuery])

  const resetPaging = () => {
    setPage(0)
    setCursors([''])
  }

  const dateRange = useMemo(() => {
    if (rangeFilter === 'all') return null
    const to = new Date()
    const from = new Date()
    if (rangeFilter === '7') from.setDate(to.getDate() - 6)
    if (rangeFilter === '30') from.setDate(to.getDate() - 29)
    return { from: isoDate(from), to: isoDate(to) }
  }, [rangeFilter])

  const params = useMemo(() => {
    const search = new URLSearchParams({ limit: '20' })
    if (deviceId) search.set('device_id', deviceId)
    if (query) search.set('q', query)
    if (modelFilter !== '__all__') search.set('model', modelFilter)
    if (streamFilter !== 'all') search.set('stream', streamFilter)
    if (statusFilter !== 'all') search.set('status', statusFilter)
    if (dateRange) {
      search.set('from', dateRange.from)
      search.set('to', dateRange.to)
    }
    if (cursors[page]) search.set('cursor', cursors[page])
    return search.toString()
  }, [deviceId, query, modelFilter, streamFilter, statusFilter, dateRange, page, cursors])

  const requests = useQuery({
    queryKey: ['ai', 'requests', params],
    queryFn: () => api<AIRequestLogPage>(`/admin/ai/requests?${params}`),
  })

  const configuredModels = useQuery({
    queryKey: ['models', 'all'],
    queryFn: () => api<{ items: ModelInfo[] }>('/admin/ai/models'),
  })

  const modelOptions = useMemo(() => {
    const set = new Set<string>()
    for (const m of requests.data?.models ?? []) {
      if (m) set.add(m)
    }
    for (const m of configuredModels.data?.items ?? []) {
      if (m.alias) set.add(m.alias)
    }
    if (modelFilter !== '__all__') set.add(modelFilter)
    return [...set].sort((a, b) => a.localeCompare(b))
  }, [requests.data?.models, configuredModels.data?.items, modelFilter])

  const hasFilters =
    Boolean(deviceId) ||
    Boolean(rawQuery.trim()) ||
    modelFilter !== '__all__' ||
    streamFilter !== 'all' ||
    statusFilter !== 'all' ||
    rangeFilter !== '7'

  const resetAll = () => {
    if (deviceId) {
      const next = new URLSearchParams(searchParams)
      next.delete('device_id')
      setSearchParams(next, { replace: true })
    }
    setRawQuery('')
    setQuery('')
    setModelFilter('__all__')
    setStreamFilter('all')
    setStatusFilter('all')
    setRangeFilter('7')
    resetPaging()
  }

  const clearDeviceFilter = () => {
    const next = new URLSearchParams(searchParams)
    next.delete('device_id')
    setSearchParams(next, { replace: true })
    resetPaging()
  }

  const items = requests.data?.items ?? []

  return (
    <>
      <PageHeader
        title="请求日志"
        description="逐条查看 AI 调用的模型、流式速度（扣除首 Token 后纯生成 TPS）、Token 构成及耗时，支持点击用户直达设备详情。"
        actions={
          <Button
            variant="outline"
            size="sm"
            onClick={() => void requests.refetch()}
            disabled={requests.isFetching}
          >
            <RotateCwIcon data-icon="inline-start" />
            刷新
          </Button>
        }
      />

      <div className="flex flex-wrap items-center gap-2">
        <InputGroup className="w-full sm:w-64">
          <InputGroupAddon>
            <SearchIcon />
          </InputGroupAddon>
          <InputGroupInput
            placeholder="搜索用户 / 设备 ID / 请求 ID"
            value={rawQuery}
            onChange={(event) => setRawQuery(event.target.value)}
          />
        </InputGroup>

        <Select
          value={modelFilter}
          onValueChange={(value) => {
            setModelFilter(value ?? '__all__')
            resetPaging()
          }}
          items={[
            { value: '__all__', label: '全部模型' },
            ...modelOptions.map((m) => ({ value: m, label: m })),
          ]}
        >
          <SelectTrigger size="sm" className="w-44" aria-label="筛选模型">
            <SelectValue placeholder="全部模型" />
          </SelectTrigger>
          <SelectContent>
            <SelectGroup>
              <SelectItem value="__all__">全部模型</SelectItem>
              {modelOptions.map((m) => (
                <SelectItem key={m} value={m}>
                  {m}
                </SelectItem>
              ))}
            </SelectGroup>
          </SelectContent>
        </Select>

        <ToggleGroup
          variant="outline"
          size="sm"
          value={[streamFilter]}
          onValueChange={(value) => {
            const next = value[0] as StreamFilter | undefined
            if (next) {
              setStreamFilter(next)
              resetPaging()
            }
          }}
        >
          <ToggleGroupItem value="all">全部方式</ToggleGroupItem>
          <ToggleGroupItem value="true">流式</ToggleGroupItem>
          <ToggleGroupItem value="false">非流式</ToggleGroupItem>
        </ToggleGroup>

        <ToggleGroup
          variant="outline"
          size="sm"
          value={[statusFilter]}
          onValueChange={(value) => {
            const next = value[0] as StatusFilter | undefined
            if (next) {
              setStatusFilter(next)
              resetPaging()
            }
          }}
        >
          <ToggleGroupItem value="all">全部状态</ToggleGroupItem>
          <ToggleGroupItem value="ok">成功</ToggleGroupItem>
          <ToggleGroupItem value="error">失败</ToggleGroupItem>
        </ToggleGroup>

        <ToggleGroup
          variant="outline"
          size="sm"
          value={[rangeFilter]}
          onValueChange={(value) => {
            const next = value[0] as RangeFilter | undefined
            if (next) {
              setRangeFilter(next)
              resetPaging()
            }
          }}
        >
          <ToggleGroupItem value="today">今天</ToggleGroupItem>
          <ToggleGroupItem value="7">近 7 天</ToggleGroupItem>
          <ToggleGroupItem value="30">近 30 天</ToggleGroupItem>
          <ToggleGroupItem value="all">全部</ToggleGroupItem>
        </ToggleGroup>

        {deviceId ? (
          <Badge variant="secondary" className="gap-1 pr-1">
            设备 {deviceId.slice(0, 8)}…
            <button
              type="button"
              onClick={clearDeviceFilter}
              aria-label="清除设备筛选"
              className="rounded-sm p-0.5 hover:bg-muted"
            >
              <XIcon className="size-3" />
            </button>
          </Badge>
        ) : null}

        {hasFilters ? (
          <Button variant="ghost" size="sm" onClick={resetAll}>
            <RotateCcwIcon data-icon="inline-start" />
            重置
          </Button>
        ) : null}

        <Badge variant="secondary" className="ml-auto tabular-nums">
          共 {formatNumber(requests.data?.total_estimate ?? 0)} 条
        </Badge>
      </div>

      <Card className="overflow-hidden py-0">
        <CardContent className="p-0">
          {requests.isLoading ? (
            <div className="flex flex-col gap-2 p-4">
              {Array.from({ length: 6 }).map((_, index) => (
                <Skeleton key={index} className="h-12 w-full" />
              ))}
            </div>
          ) : requests.isError ? (
            <div role="alert" className="flex flex-wrap items-center gap-3 p-6 text-sm">
              <p>请求日志加载失败：{errorMessage(requests.error)}</p>
              <Button size="sm" variant="outline" onClick={() => void requests.refetch()}>
                重试
              </Button>
            </div>
          ) : items.length === 0 ? (
            <Empty className="border-0">
              <EmptyHeader>
                <EmptyMedia variant="icon">
                  <ScrollTextIcon />
                </EmptyMedia>
                <EmptyTitle>没有匹配的请求记录</EmptyTitle>
                <EmptyDescription>
                  尝试调整时间范围、模型或流式筛选条件。
                </EmptyDescription>
              </EmptyHeader>
            </Empty>
          ) : (
            <>
              <div className="divide-y md:hidden">
                {items.map((item) => (
                  <div key={item.id} className="flex flex-col gap-2.5 p-4 text-sm">
                    <div className="flex items-start justify-between gap-2">
                      <RequestUserCell item={item} />
                      <div className="flex flex-col items-end gap-1">
                        <span className="text-xs tabular-nums text-muted-foreground">
                          {formatDateTimeSeconds(item.created_at)}
                        </span>
                        {item.status === 'error' ? (
                          <Badge variant="destructive">
                            {item.error_code || `HTTP ${item.http_status}`}
                          </Badge>
                        ) : null}
                      </div>
                    </div>
                    <div className="flex items-center justify-between gap-2 rounded-md bg-muted/40 px-2.5 py-1.5">
                      <div>
                        <p className="font-medium">{item.model}</p>
                        {item.provider_name ? (
                          <p className="text-xs text-muted-foreground">{item.provider_name}</p>
                        ) : null}
                      </div>
                      <StreamTpsCell item={item} />
                    </div>
                    <div className="flex items-center justify-between gap-2">
                      <TokenCell item={item} />
                      <LatencyCell item={item} />
                    </div>
                  </div>
                ))}
              </div>

              <div className="hidden md:block">
                <Table>
                  <TableHeader>
                    <TableRow>
                      <TableHead>时间</TableHead>
                      <TableHead>用户</TableHead>
                      <TableHead>模型</TableHead>
                      <TableHead>流式 / TPS</TableHead>
                      <TableHead>Token（入/出/缓）</TableHead>
                      <TableHead className="text-right">耗时（首/总）</TableHead>
                    </TableRow>
                  </TableHeader>
                  <TableBody>
                    {items.map((item) => (
                      <TableRow key={item.id}>
                        <TableCell>
                          <div className="flex flex-col gap-0.5">
                            <Tooltip>
                              <TooltipTrigger
                                render={
                                  <span className="w-fit cursor-default tabular-nums">
                                    {formatDateTimeSeconds(item.created_at)}
                                  </span>
                                }
                              />
                              <TooltipContent>
                                <div className="flex flex-col gap-0.5 font-mono text-[11px]">
                                  <span>{formatFullDateTime(item.created_at)}</span>
                                  <span>{item.request_id}</span>
                                </div>
                              </TooltipContent>
                            </Tooltip>
                            {item.status === 'error' ? (
                              <div className="flex items-center gap-1">
                                <Badge variant="destructive" className="px-1.5 py-0 text-[10px]">
                                  失败
                                </Badge>
                                <span className="font-mono text-xs text-destructive">
                                  {item.error_code || item.http_status}
                                </span>
                              </div>
                            ) : null}
                          </div>
                        </TableCell>
                        <TableCell>
                          <RequestUserCell item={item} />
                        </TableCell>
                        <TableCell>
                          <div className="flex flex-col gap-0.5">
                            <span className="font-medium">{item.model}</span>
                            <span className="text-xs text-muted-foreground">
                              {item.provider_name || '—'}
                            </span>
                          </div>
                        </TableCell>
                        <TableCell>
                          <StreamTpsCell item={item} />
                        </TableCell>
                        <TableCell>
                          <TokenCell item={item} />
                        </TableCell>
                        <TableCell className="text-right">
                          <LatencyCell item={item} />
                        </TableCell>
                      </TableRow>
                    ))}
                  </TableBody>
                </Table>
              </div>
            </>
          )}
        </CardContent>
      </Card>

      <div className="flex items-center justify-between">
        <span className="text-xs text-muted-foreground">第 {page + 1} 页 · 每页 20 条</span>
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
            disabled={!requests.data?.has_more}
            onClick={() => {
              const next = requests.data?.next_cursor
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
    </>
  )
}
