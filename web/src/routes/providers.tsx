import { useState } from 'react'
import { useForm } from 'react-hook-form'
import { zodResolver } from '@hookform/resolvers/zod'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { z } from 'zod'
import { toast } from 'sonner'
import {
  BotIcon,
  CheckCircle2Icon,
  PencilIcon,
  PlugZapIcon,
  PlusIcon,
  RefreshCwIcon,
  XCircleIcon,
} from 'lucide-react'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Checkbox } from '@/components/ui/checkbox'
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
  Empty,
  EmptyContent,
  EmptyDescription,
  EmptyHeader,
  EmptyMedia,
  EmptyTitle,
} from '@/components/ui/empty'
import {
  Field,
  FieldDescription,
  FieldError,
  FieldGroup,
  FieldLabel,
} from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import { Separator } from '@/components/ui/separator'
import { Skeleton } from '@/components/ui/skeleton'
import { Spinner } from '@/components/ui/spinner'
import { Switch } from '@/components/ui/switch'
import { PageHeader } from '@/components/page-header'
import { api, errorMessage } from '@/lib/api'
import { formatDateTime } from '@/lib/format'
import type { Provider } from '@/lib/types'

const providerSchema = z.object({
  name: z.string().min(1, '请填写名称').max(64),
  base_url: z.url('请填写合法的接口地址，例如 https://api.deepseek.com/v1'),
  api_key: z.string().max(512).optional(),
  enabled: z.boolean(),
  timeout_seconds: z.number().int().min(5, '至少 5 秒').max(600, '最多 600 秒'),
})

type ProviderForm = z.infer<typeof providerSchema>

const defaults: ProviderForm = {
  name: '',
  base_url: 'https://api.deepseek.com/v1',
  api_key: '',
  enabled: true,
  timeout_seconds: 120,
}

export function ProvidersPage() {
  const queryClient = useQueryClient()
  const [open, setOpen] = useState(false)
  const [editing, setEditing] = useState<Provider | null>(null)
  const [missing, setMissing] = useState<{ id: string; model_id: string; display_name: string }[]>([])
  const [selected, setSelected] = useState<string[]>([])

  const providers = useQuery({
    queryKey: ['providers'],
    queryFn: () => api<{ items: Provider[] }>('/admin/ai/providers'),
  })

  const form = useForm<ProviderForm>({
    resolver: zodResolver(providerSchema),
    defaultValues: defaults,
  })

  const openCreate = () => {
    setEditing(null)
    form.reset(defaults)
    setOpen(true)
  }

  const openEdit = (provider: Provider) => {
    setEditing(provider)
    form.reset({
      name: provider.name,
      base_url: provider.base_url,
      api_key: '',
      enabled: provider.enabled,
      timeout_seconds: provider.timeout_seconds,
    })
    setOpen(true)
  }

  const save = useMutation({
    mutationFn: (values: ProviderForm) => {
      const body = {
        name: values.name.trim(),
        base_url: values.base_url.trim(),
        api_key: values.api_key?.trim() ?? '',
        enabled: values.enabled,
        timeout_seconds: values.timeout_seconds,
      }
      if (editing) {
        return api(`/admin/ai/providers/${editing.id}`, { method: 'PATCH', body })
      }
      return api('/admin/ai/providers', { method: 'POST', body })
    },
    onSuccess: () => {
      toast.success(editing ? '供应商已更新' : '供应商已创建')
      setOpen(false)
      void queryClient.invalidateQueries({ queryKey: ['providers'] })
    },
    onError: (error) => toast.error(errorMessage(error)),
  })

  const test = useMutation({
    mutationFn: (id: string) =>
      api<{ ok: boolean; error: string }>(`/admin/ai/providers/${id}/test`, { method: 'POST' }),
    onSuccess: (result) => {
      if (result.ok) toast.success('连接正常')
      else toast.error(`连接失败：${result.error || '未知原因'}`)
      void queryClient.invalidateQueries({ queryKey: ['providers'] })
    },
    onError: (error) => toast.error(errorMessage(error)),
  })

  const sync = useMutation({
    mutationFn: (id: string) =>
      api<{ synced: number; missing: { id: string; model_id: string; display_name: string }[] }>(
        `/admin/ai/providers/${id}/sync-models`,
        { method: 'POST' },
      ),
    onSuccess: (result) => {
      void queryClient.invalidateQueries({ queryKey: ['providers'] })
      void queryClient.invalidateQueries({ queryKey: ['models'] })
      void queryClient.invalidateQueries({ queryKey: ['ai'] })
      if (result.missing.length > 0) {
        setMissing(result.missing)
        setSelected(result.missing.map((item) => item.id))
        toast.info(`同步完成，有 ${result.missing.length} 个模型上游已不再提供`)
      } else {
        toast.success(`已同步 ${result.synced} 个模型`)
      }
    },
    onError: (error) => toast.error(errorMessage(error)),
  })

  const bulkDelete = useMutation({
    mutationFn: (ids: string[]) =>
      api<{ deleted: number }>('/admin/ai/models/bulk-delete', { method: 'POST', body: { ids } }),
    onSuccess: (result) => {
      toast.success(`已删除 ${result.deleted} 个模型`)
      setMissing([])
      setSelected([])
      void queryClient.invalidateQueries({ queryKey: ['models'] })
      void queryClient.invalidateQueries({ queryKey: ['ai'] })
    },
    onError: (error) => toast.error(errorMessage(error)),
  })

  // 卡片上的启停开关，直接带全量字段提交，避免把超时等配置覆盖成 0
  const toggleProvider = useMutation({
    mutationFn: (vars: { provider: Provider; enabled: boolean }) =>
      api(`/admin/ai/providers/${vars.provider.id}`, {
        method: 'PATCH',
        body: {
          name: vars.provider.name,
          base_url: vars.provider.base_url,
          timeout_seconds: vars.provider.timeout_seconds,
          enabled: vars.enabled,
        },
      }),
    onSuccess: (_data, vars) => {
      toast.success(vars.enabled ? '供应商已启用' : '供应商已停用，其模型不再对客户端可见')
      void queryClient.invalidateQueries({ queryKey: ['providers'] })
      void queryClient.invalidateQueries({ queryKey: ['models'] })
      void queryClient.invalidateQueries({ queryKey: ['ai'] })
    },
    onError: (error) => toast.error(errorMessage(error)),
  })

  const items = providers.data?.items ?? []

  return (
    <>
      <PageHeader
        title="AI 供应商"
        description="上游密钥只会加密存储在服务端，客户端只能拿到模型名与限额。"
        actions={
          <Dialog open={open} onOpenChange={setOpen}>
            <DialogTrigger render={<Button size="sm" onClick={openCreate} />}>
              <PlusIcon data-icon="inline-start" />
              新建供应商
            </DialogTrigger>
            <DialogContent>
              <DialogHeader>
                <DialogTitle>{editing ? '编辑供应商' : '新建供应商'}</DialogTitle>
                <DialogDescription>
                  兼容 OpenAI 协议的网关均可接入；保存后先「探活」再「同步模型」。
                </DialogDescription>
              </DialogHeader>

              <form
                onSubmit={form.handleSubmit((values) => save.mutate(values))}
                className="flex flex-col gap-4"
              >
                <FieldGroup>
                  <Field data-invalid={Boolean(form.formState.errors.name)}>
                    <FieldLabel htmlFor="provider-name">名称</FieldLabel>
                    <Input
                      id="provider-name"
                      placeholder="deepseek"
                      aria-invalid={Boolean(form.formState.errors.name)}
                      {...form.register('name')}
                    />
                    <FieldError>{form.formState.errors.name?.message}</FieldError>
                  </Field>

                  <Field data-invalid={Boolean(form.formState.errors.base_url)}>
                    <FieldLabel htmlFor="provider-base-url">接口地址</FieldLabel>
                    <Input
                      id="provider-base-url"
                      placeholder="https://api.deepseek.com/v1"
                      aria-invalid={Boolean(form.formState.errors.base_url)}
                      {...form.register('base_url')}
                    />
                    <FieldDescription>
                      不含 /chat/completions；服务端会自行拼接 /models 与 /chat/completions。
                    </FieldDescription>
                    <FieldError>{form.formState.errors.base_url?.message}</FieldError>
                  </Field>

                  <Field data-invalid={Boolean(form.formState.errors.api_key)}>
                    <FieldLabel htmlFor="provider-api-key">
                      密钥 {editing ? '（留空则不修改）' : ''}
                    </FieldLabel>
                    <Input
                      id="provider-api-key"
                      type="password"
                      autoComplete="off"
                      placeholder={editing ? 'sk-****' : 'sk-...'}
                      aria-invalid={Boolean(form.formState.errors.api_key)}
                      {...form.register('api_key')}
                    />
                    <FieldDescription>使用 AES-256-GCM 加密后落库，接口只回显末四位。</FieldDescription>
                    <FieldError>{form.formState.errors.api_key?.message}</FieldError>
                  </Field>

                  <Field>
                    <FieldLabel htmlFor="provider-timeout">超时（秒）</FieldLabel>
                    <Input
                      id="provider-timeout"
                      type="number"
                      min={5}
                      max={600}
                      {...form.register('timeout_seconds', { valueAsNumber: true })}
                    />
                    <FieldError>{form.formState.errors.timeout_seconds?.message}</FieldError>
                  </Field>

                </FieldGroup>

                <DialogFooter>
                  <Button type="submit" disabled={save.isPending}>
                    {save.isPending ? <Spinner data-icon="inline-start" /> : null}
                    保存
                  </Button>
                </DialogFooter>
              </form>
            </DialogContent>
          </Dialog>
        }
      />

      {providers.isLoading ? (
        <div className="grid gap-4 md:grid-cols-2">
          <Skeleton className="h-48 w-full" />
          <Skeleton className="h-48 w-full" />
        </div>
      ) : providers.isError ? (
        <div role="alert" className="flex flex-wrap items-center gap-3 rounded-lg border p-4 text-sm">
          <p>供应商加载失败：{errorMessage(providers.error)}</p>
          <Button size="sm" variant="outline" onClick={() => void providers.refetch()}>重试</Button>
        </div>
      ) : items.length === 0 ? (
        <Empty className="border">
          <EmptyHeader>
            <EmptyMedia variant="icon">
              <BotIcon />
            </EmptyMedia>
            <EmptyTitle>还没有供应商</EmptyTitle>
            <EmptyDescription>
              添加一个 OpenAI 兼容的上游，同步模型并启用后，客户端即可使用 AI。
            </EmptyDescription>
          </EmptyHeader>
          <EmptyContent>
            <Button size="sm" onClick={openCreate}>
              <PlusIcon data-icon="inline-start" />
              新建供应商
            </Button>
          </EmptyContent>
        </Empty>
      ) : (
        <div className="grid gap-4 md:grid-cols-2">
          {items.map((provider) => {
            return (
              <Card key={provider.id}>
                <CardHeader>
                  <CardTitle className="flex items-center gap-2">
                    <span className="min-w-0 break-all">{provider.name}</span>
                    {!provider.enabled ? <Badge variant="outline">已停用</Badge> : null}
                  </CardTitle>
                  <CardDescription className="truncate font-mono text-xs">
                    {provider.base_url}
                  </CardDescription>
                </CardHeader>
                <CardContent className="flex flex-col gap-3">
                  <div className="flex items-center justify-between text-sm">
                    <span className="text-muted-foreground">密钥</span>
                    <span className="font-mono text-xs">{provider.api_key_masked}</span>
                  </div>
                  <div className="flex items-center justify-between text-sm">
                    <span className="text-muted-foreground">上次探活</span>
                    <span className="flex items-center gap-1.5 text-xs">
                      {provider.last_check_ok === true ? (
                        <CheckCircle2Icon className="size-3.5 text-primary" aria-hidden />
                      ) : provider.last_check_ok === false ? (
                        <XCircleIcon className="size-3.5 text-destructive" aria-hidden />
                      ) : null}
                      {provider.last_check_at ? formatDateTime(provider.last_check_at) : '未检测'}
                    </span>
                  </div>
                  {provider.last_check_error ? (
                    <p className="line-clamp-2 text-xs text-destructive">{provider.last_check_error}</p>
                  ) : null}

                  <Separator />

                  <div className="flex flex-wrap items-center gap-2">
                    <Button
                      variant="outline"
                      size="sm"
                      disabled={test.isPending}
                      onClick={() => test.mutate(provider.id)}
                    >
                      <PlugZapIcon data-icon="inline-start" />
                      探活
                    </Button>
                    <Button
                      variant="outline"
                      size="sm"
                      disabled={sync.isPending}
                      onClick={() => sync.mutate(provider.id)}
                    >
                      <RefreshCwIcon data-icon="inline-start" />
                      同步模型
                    </Button>
                    <Button
                      variant="ghost"
                      size="sm"
                      className="ml-auto"
                      onClick={() => openEdit(provider)}
                    >
                      <PencilIcon data-icon="inline-start" />
                      编辑
                    </Button>
                    <div className="flex items-center gap-2 pl-2">
                      <Switch
                        checked={provider.enabled}
                        disabled={toggleProvider.isPending}
                        onCheckedChange={(checked) =>
                          toggleProvider.mutate({ provider, enabled: checked })
                        }
                        aria-label={`启用 ${provider.name}`}
                      />
                      <span className="text-xs text-muted-foreground">
                        {provider.enabled ? '启用' : '停用'}
                      </span>
                    </div>
                  </div>
                </CardContent>
              </Card>
            )
          })}
        </div>
      )}

      <Dialog
        open={missing.length > 0}
        onOpenChange={(next) => {
          if (!next) {
            setMissing([])
            setSelected([])
          }
        }}
      >
        <DialogContent>
          <DialogHeader>
            <DialogTitle>有 {missing.length} 个模型上游已不再提供</DialogTitle>
            <DialogDescription>
              勾选需要删除的模型，默认全选。不删除的会继续保留在列表里，可以稍后手动处理。
            </DialogDescription>
          </DialogHeader>

          <div className="flex items-center justify-between">
            <Button
              variant="ghost"
              size="xs"
              onClick={() =>
                setSelected(selected.length === missing.length ? [] : missing.map((item) => item.id))
              }
            >
              {selected.length === missing.length ? '取消全选' : '全选'}
            </Button>
            <span className="text-xs text-muted-foreground tabular-nums">
              已选 {selected.length} / {missing.length}
            </span>
          </div>

          <div className="flex max-h-64 flex-col gap-2 overflow-y-auto rounded-lg border p-3">
            {missing.map((item) => (
              <label key={item.id} className="flex items-center gap-2.5 text-sm">
                <Checkbox
                  checked={selected.includes(item.id)}
                  onCheckedChange={(checked) =>
                    setSelected((prev) =>
                      checked ? [...prev, item.id] : prev.filter((value) => value !== item.id),
                    )
                  }
                />
                <span className="font-medium">{item.display_name || item.model_id}</span>
                <span className="font-mono text-xs text-muted-foreground">{item.model_id}</span>
              </label>
            ))}
          </div>

          <DialogFooter>
            <Button
              variant="outline"
              onClick={() => {
                setMissing([])
                setSelected([])
              }}
            >
              保留
            </Button>
            <Button
              variant="destructive"
              disabled={selected.length === 0 || bulkDelete.isPending}
              onClick={() => bulkDelete.mutate(selected)}
            >
              {bulkDelete.isPending ? '删除中…' : `删除所选（${selected.length}）`}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </>
  )
}
