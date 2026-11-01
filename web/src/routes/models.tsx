import { useMemo, useState } from 'react'
import { useForm } from 'react-hook-form'
import { zodResolver } from '@hookform/resolvers/zod'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { z } from 'zod'
import { toast } from 'sonner'
import { Link } from 'react-router'
import { CpuIcon, MoreHorizontalIcon, PlusIcon, SparklesIcon, Trash2Icon } from 'lucide-react'
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from '@/components/ui/alert-dialog'
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
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
  DialogTrigger,
} from '@/components/ui/dialog'
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu'
import { Empty, EmptyDescription, EmptyHeader, EmptyMedia, EmptyTitle } from '@/components/ui/empty'
import { Field, FieldDescription, FieldError, FieldGroup, FieldLabel } from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { Skeleton } from '@/components/ui/skeleton'
import { Spinner } from '@/components/ui/spinner'
import { Switch } from '@/components/ui/switch'
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
import { formatNumber } from '@/lib/format'
import type { ActiveAI, ModelInfo, Provider } from '@/lib/types'

const modelSchema = z.object({
  provider_id: z.string().min(1, '请选择供应商'),
  model_id: z.string().min(1, '请填写上游模型 ID').max(128),
  alias: z.string().max(128).optional(),
  display_name: z.string().max(128).optional(),
  context_window: z.number().int().min(0).max(10_000_000).optional(),
})

type ModelForm = z.infer<typeof modelSchema>

const defaults: ModelForm = {
  provider_id: '',
  model_id: '',
  alias: '',
  display_name: '',
  context_window: 65536,
}

/** 同一个上游模型可能被多家供应商提供，按 model_id 归并成一行。 */
interface ModelGroup {
  modelId: string
  rows: ModelInfo[]
  /** 当前生效的那行；全关时取第一行用于展示 */
  active: ModelInfo
  enabled: boolean
}

export function ModelsPage() {
  const queryClient = useQueryClient()
  const [open, setOpen] = useState(false)
  const [pendingDelete, setPendingDelete] = useState<ModelGroup | null>(null)

  const providers = useQuery({
    queryKey: ['providers'],
    queryFn: () => api<{ items: Provider[] }>('/admin/ai/providers'),
  })

  const models = useQuery({
    queryKey: ['models', 'all'],
    queryFn: () => api<{ items: ModelInfo[] }>('/admin/ai/models'),
  })

  const active = useQuery({
    queryKey: ['ai', 'active'],
    queryFn: () => api<{ active: ActiveAI | null }>('/admin/ai/active'),
  })

  const settings = useQuery({
    queryKey: ['settings'],
    queryFn: () =>
      api<{ effective: Record<string, number | string> }>('/admin/settings'),
  })

  const form = useForm<ModelForm>({
    resolver: zodResolver(modelSchema),
    defaultValues: defaults,
  })

  const invalidate = () => {
    void queryClient.invalidateQueries({ queryKey: ['models'] })
    void queryClient.invalidateQueries({ queryKey: ['ai'] })
  }

  const create = useMutation({
    mutationFn: (values: ModelForm) =>
      api('/admin/ai/models', {
        method: 'POST',
        body: {
          provider_id: values.provider_id,
          model_id: values.model_id.trim(),
          alias: values.alias?.trim() || values.model_id.trim(),
          display_name: values.display_name?.trim() || values.model_id.trim(),
          context_window: values.context_window ?? 0,
          enabled: true,
        },
      }),
    onSuccess: () => {
      toast.success('模型已添加')
      setOpen(false)
      form.reset(defaults)
      invalidate()
    },
    onError: (error) => toast.error(errorMessage(error)),
  })

  // 以模型为单位启停 / 切换供应商
  const setAvailability = useMutation({
    mutationFn: (vars: { modelId: string; enabled: boolean; providerId?: string }) =>
      api('/admin/ai/models/availability', {
        method: 'PUT',
        body: { model_id: vars.modelId, enabled: vars.enabled, provider_id: vars.providerId },
      }),
    onSuccess: invalidate,
    onError: (error) => toast.error(errorMessage(error)),
  })

  const setDefault = useMutation({
    mutationFn: (modelId: string) =>
      api('/admin/ai/models/default', { method: 'PUT', body: { model_id: modelId } }),
    onSuccess: () => {
      toast.success('已设为默认模型')
      invalidate()
    },
    onError: (error) => toast.error(errorMessage(error)),
  })

  const detectReasoning = useMutation({
    mutationFn: (modelId: string) =>
      api<{ reasoning_mode: string; reasoning_efforts: string[] | null }>(
        `/admin/ai/models/${modelId}/detect-reasoning`,
        { method: 'POST' },
      ),
    onSuccess: (result) => {
      const label =
        result.reasoning_mode === 'effort'
          ? '可设置思考强度'
          : result.reasoning_mode === 'fixed'
            ? '固定带思考'
            : '不支持思考强度'
      toast.success(`检测完成：${label}`)
      invalidate()
    },
    onError: (error) => toast.error(errorMessage(error)),
  })

  // 默认思考强度是全局设置，所有支持档位的模型共用
  const setGlobalEffort = useMutation({
    mutationFn: (effort: string) =>
      api('/admin/settings', {
        method: 'PUT',
        body: { values: { 'ai.reasoning_effort': effort } },
      }),
    onSuccess: () => {
      toast.success('默认思考强度已更新')
      void queryClient.invalidateQueries({ queryKey: ['settings'] })
      void queryClient.invalidateQueries({ queryKey: ['ai'] })
    },
    onError: (error) => toast.error(errorMessage(error)),
  })

  const remove = useMutation({
    mutationFn: (ids: string[]) =>
      api<{ deleted: number }>('/admin/ai/models/bulk-delete', { method: 'POST', body: { ids } }),
    onSuccess: (result) => {
      toast.success(`已删除 ${result.deleted} 条模型记录`)
      setPendingDelete(null)
      invalidate()
    },
    onError: (error) => toast.error(errorMessage(error)),
  })

  const groups = useMemo<ModelGroup[]>(() => {
    const map = new Map<string, ModelInfo[]>()
    for (const item of models.data?.items ?? []) {
      const list = map.get(item.model_id)
      if (list) list.push(item)
      else map.set(item.model_id, [item])
    }
    return [...map.entries()]
      .map(([modelId, rows]) => {
        const active = rows.find((row) => row.enabled) ?? rows[0]
        return { modelId, rows, active, enabled: rows.some((row) => row.enabled) }
      })
      .sort((a, b) => a.modelId.localeCompare(b.modelId))
  }, [models.data])

  const defaultModelId = active.data?.active?.model_id ?? ''
  const globalEffort = String(settings.data?.effective?.['ai.reasoning_effort'] ?? '')
  const enabledGroups = groups.filter((group) => group.enabled)
  const defaultGroup = groups.find((group) => group.active.id === defaultModelId)
  const enabledCount = groups.filter((group) => group.enabled).length
  const providerItems = (providers.data?.items ?? []).filter((provider) => provider.enabled)

  return (
    <>
      <PageHeader
        title="模型"
        description={
          groups.length > 0
            ? `一个模型一行，开关控制客户端能否使用；同一个模型被多家供应商提供时，用供应商下拉切换走哪家。当前可用 ${enabledCount} / ${groups.length}。`
            : '先在「供应商」里同步模型，或在这里手动添加。'
        }
        actions={
          <div className="flex flex-wrap items-center gap-2">
            <div className="flex items-center gap-1.5">
              <span className="text-xs text-muted-foreground">默认模型</span>
              <Select
                value={defaultModelId}
                onValueChange={(value) => value && setDefault.mutate(value)}
                disabled={enabledGroups.length === 0 || setDefault.isPending}
                items={enabledGroups.map((group) => ({
                  value: group.active.id,
                  label: group.active.display_name || group.modelId,
                }))}
              >
                <SelectTrigger size="sm" className="w-40">
                  <SelectValue placeholder="未设置" />
                </SelectTrigger>
                <SelectContent>
                  <SelectGroup>
                    {enabledGroups.map((group) => (
                      <SelectItem key={group.active.id} value={group.active.id}>
                        {group.active.display_name || group.modelId}
                      </SelectItem>
                    ))}
                  </SelectGroup>
                </SelectContent>
              </Select>
            </div>

            <div className="flex items-center gap-1.5">
              <span className="text-xs text-muted-foreground">默认强度</span>
              <Select
                value={globalEffort || '__none__'}
                onValueChange={(value) =>
                  setGlobalEffort.mutate(value === '__none__' ? '' : (value ?? ''))
                }
                disabled={defaultGroup?.active.reasoning_mode !== 'effort' || setGlobalEffort.isPending}
                items={[
                  { value: '__none__', label: '不指定' },
                  ...(defaultGroup?.active.reasoning_efforts ?? ['minimal', 'low', 'medium', 'high']).map(
                    (item) => ({ value: item, label: item }),
                  ),
                ]}
              >
                <SelectTrigger size="sm" className="w-32">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectGroup>
                    <SelectItem value="__none__">不指定</SelectItem>
                    {(defaultGroup?.active.reasoning_efforts ?? ['minimal', 'low', 'medium', 'high']).map(
                      (item) => (
                        <SelectItem key={item} value={item}>
                          {item}
                        </SelectItem>
                      ),
                    )}
                  </SelectGroup>
                </SelectContent>
              </Select>
            </div>

            <Dialog open={open} onOpenChange={setOpen}>
            <DialogTrigger
              render={
                <Button
                  size="sm"
                  disabled={providerItems.length === 0}
                  onClick={() => {
                    form.reset({
                      ...defaults,
                      provider_id: providerItems[0]?.id ?? '',
                    })
                  }}
                />
              }
            >
              <PlusIcon data-icon="inline-start" />
              手动添加
            </DialogTrigger>
            <DialogContent>
              <DialogHeader>
                <DialogTitle>手动添加模型</DialogTitle>
                <DialogDescription>
                  上游 /models 没返回、但实际可用的模型可以用这里补齐。
                </DialogDescription>
              </DialogHeader>
              <form
                onSubmit={form.handleSubmit((values) => create.mutate(values))}
                className="flex flex-col gap-4"
              >
                <FieldGroup>
                  <Field data-invalid={Boolean(form.formState.errors.provider_id)}>
                    <FieldLabel>供应商</FieldLabel>
                    <Select
                      value={form.watch('provider_id')}
                      onValueChange={(value) => form.setValue('provider_id', value ?? '')}
                      items={providerItems.map((provider) => ({
                        value: provider.id,
                        label: provider.name,
                      }))}
                    >
                      <SelectTrigger className="w-full">
                        <SelectValue placeholder="选择供应商" />
                      </SelectTrigger>
                      <SelectContent>
                        <SelectGroup>
                          {providerItems.map((provider) => (
                            <SelectItem key={provider.id} value={provider.id}>
                              {provider.name}
                            </SelectItem>
                          ))}
                        </SelectGroup>
                      </SelectContent>
                    </Select>
                    <FieldError>{form.formState.errors.provider_id?.message}</FieldError>
                  </Field>

                  <Field data-invalid={Boolean(form.formState.errors.model_id)}>
                    <FieldLabel htmlFor="model-id">模型 ID</FieldLabel>
                    <Input
                      id="model-id"
                      placeholder="deepseek-chat"
                      aria-invalid={Boolean(form.formState.errors.model_id)}
                      {...form.register('model_id')}
                    />
                    <FieldDescription>
                      上游请求体里 model 字段的原始值；如果别的供应商也有同名模型，会自动合并成一行。
                    </FieldDescription>
                    <FieldError>{form.formState.errors.model_id?.message}</FieldError>
                  </Field>

                  <Field>
                    <FieldLabel htmlFor="model-alias">别名</FieldLabel>
                    <Input
                      id="model-alias"
                      placeholder="留空则与模型 ID 相同"
                      {...form.register('alias')}
                    />
                  </Field>

                  <Field>
                    <FieldLabel htmlFor="model-display">展示名</FieldLabel>
                    <Input
                      id="model-display"
                      placeholder="DeepSeek Chat"
                      {...form.register('display_name')}
                    />
                  </Field>

                  <Field>
                    <FieldLabel htmlFor="model-context">上下文长度</FieldLabel>
                    <Input
                      id="model-context"
                      type="number"
                      min={0}
                      {...form.register('context_window', { valueAsNumber: true })}
                    />
                  </Field>
                </FieldGroup>
                <DialogFooter>
                  <Button type="submit" disabled={create.isPending}>
                    {create.isPending ? <Spinner data-icon="inline-start" /> : null}
                    添加
                  </Button>
                </DialogFooter>
              </form>
              </DialogContent>
            </Dialog>
          </div>
        }
      />

      <Card className="overflow-hidden py-0">
        <CardHeader className="px-4 pt-4">
          <CardTitle>模型清单</CardTitle>
          <CardDescription>同一模型出现在多家供应商时合并展示，开关与供应商切换即时生效</CardDescription>
        </CardHeader>
        <CardContent className="p-0">
          {models.isLoading ? (
            <div className="flex flex-col gap-2 p-4">
              {Array.from({ length: 5 }).map((_, index) => (
                <Skeleton key={index} className="h-10 w-full" />
              ))}
            </div>
          ) : groups.length === 0 ? (
            <Empty className="border-0">
              <EmptyHeader>
                <EmptyMedia variant="icon">
                  <CpuIcon />
                </EmptyMedia>
                <EmptyTitle>暂无模型</EmptyTitle>
                <EmptyDescription>
                  回到供应商页点击「同步模型」，或在这里手动添加。
                </EmptyDescription>
              </EmptyHeader>
              <EmptyDescription>
                <Link to="/ai/providers" className="underline underline-offset-4">
                  去供应商页
                </Link>
              </EmptyDescription>
            </Empty>
          ) : (
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>模型</TableHead>
                  <TableHead>供应商</TableHead>
                  <TableHead className="hidden md:table-cell">思考</TableHead>
                  <TableHead className="hidden lg:table-cell text-right">上下文</TableHead>
                  <TableHead className="hidden sm:table-cell">来源</TableHead>
                  <TableHead className="text-center">可用</TableHead>
                  <TableHead className="text-right">操作</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {groups.map((group) => (
                  <TableRow key={group.modelId}>
                    <TableCell>
                      <div className="flex flex-col gap-0.5">
                        <span className="flex items-center gap-2">
                          <span className="font-medium">
                            {group.active.display_name || group.modelId}
                          </span>
                          {group.active.id === defaultModelId ? (
                            <Badge variant="secondary">默认</Badge>
                          ) : null}
                        </span>
                        <span className="flex items-center gap-2">
                          <span className="font-mono text-xs text-muted-foreground">
                            {group.modelId}
                          </span>
                          {group.rows.length > 1 ? (
                            <Badge variant="outline">{group.rows.length} 家提供</Badge>
                          ) : null}
                        </span>
                      </div>
                    </TableCell>
                    <TableCell>
                      {group.rows.length > 1 ? (
                        <Select
                          value={group.active.provider_id}
                          onValueChange={(value) =>
                            setAvailability.mutate({
                              modelId: group.modelId,
                              enabled: true,
                              providerId: value ?? undefined,
                            })
                          }
                          items={group.rows.map((row) => ({
                            value: row.provider_id,
                            label: row.provider_name,
                          }))}
                        >
                          <SelectTrigger size="sm" className="w-36">
                            <SelectValue className="truncate" />
                          </SelectTrigger>
                          <SelectContent>
                            <SelectGroup>
                              {group.rows.map((row) => (
                                <SelectItem key={row.id} value={row.provider_id}>
                                  {row.provider_name}
                                </SelectItem>
                              ))}
                            </SelectGroup>
                          </SelectContent>
                        </Select>
                      ) : (
                        <span className="text-sm text-muted-foreground">
                          {group.active.provider_name}
                        </span>
                      )}
                    </TableCell>
                    <TableCell className="hidden md:table-cell">
                      <div className="flex flex-col gap-0.5">
                        {group.active.reasoning_mode === 'effort' ? (
                          <Badge variant="secondary">可设置</Badge>
                        ) : group.active.reasoning_mode === 'fixed' ? (
                          <Badge variant="secondary">固定开启</Badge>
                        ) : group.active.reasoning_mode === 'unknown' ? (
                          <Badge variant="outline" className="text-muted-foreground">
                            未检测
                          </Badge>
                        ) : (
                          <Badge variant="outline" className="text-muted-foreground">
                            不支持
                          </Badge>
                        )}
                      </div>
                    </TableCell>
                    <TableCell className="hidden lg:table-cell text-right tabular-nums">
                      {group.active.context_window ? formatNumber(group.active.context_window) : '—'}
                    </TableCell>
                    <TableCell className="hidden sm:table-cell">
                      <Badge variant="outline">
                        {group.active.source === 'manual' ? '手动' : '同步'}
                      </Badge>
                    </TableCell>
                    <TableCell className="text-center">
                      <Switch
                        checked={group.enabled}
                        disabled={setAvailability.isPending}
                        onCheckedChange={(checked) =>
                          setAvailability.mutate({ modelId: group.modelId, enabled: checked })
                        }
                        aria-label={`启用 ${group.modelId}`}
                      />
                    </TableCell>
                    <TableCell className="text-right">
                      <DropdownMenu>
                        <DropdownMenuTrigger
                          render={
                            <Button
                              variant="ghost"
                              size="icon-sm"
                              aria-label="更多操作"
                              disabled={setDefault.isPending}
                            >
                              <MoreHorizontalIcon />
                            </Button>
                          }
                        />
                        <DropdownMenuContent align="end">
                          <DropdownMenuItem
                            disabled={detectReasoning.isPending}
                            onClick={() => detectReasoning.mutate(group.active.id)}
                          >
                            <SparklesIcon />
                            检测思考能力
                          </DropdownMenuItem>
                          <DropdownMenuSeparator />
                          <DropdownMenuItem variant="destructive" onClick={() => setPendingDelete(group)}>
                            <Trash2Icon />
                            删除
                          </DropdownMenuItem>
                        </DropdownMenuContent>
                      </DropdownMenu>
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          )}
        </CardContent>
      </Card>

      <AlertDialog
        open={pendingDelete !== null}
        onOpenChange={(next) => {
          if (!next) setPendingDelete(null)
        }}
      >
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>删除模型「{pendingDelete?.modelId}」？</AlertDialogTitle>
            <AlertDialogDescription>
              {pendingDelete && pendingDelete.rows.length > 1
                ? `该模型在 ${pendingDelete.rows.length} 家供应商下都有记录，会一并删除。`
                : '删除后需要重新同步或手动添加才能恢复。'}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>取消</AlertDialogCancel>
            <AlertDialogAction
              onClick={() => {
                if (pendingDelete) remove.mutate(pendingDelete.rows.map((row) => row.id))
              }}
            >
              删除
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </>
  )
}
