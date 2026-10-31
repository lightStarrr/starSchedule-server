import { useState } from 'react'
import { useForm } from 'react-hook-form'
import { zodResolver } from '@hookform/resolvers/zod'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { z } from 'zod'
import { toast } from 'sonner'
import { Link } from 'react-router'
import { CpuIcon, PlusIcon, Trash2Icon } from 'lucide-react'
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
import { Empty, EmptyDescription, EmptyHeader, EmptyMedia, EmptyTitle } from '@/components/ui/empty'
import { Field, FieldDescription, FieldError, FieldGroup, FieldLabel } from '@/components/ui/field'
import { Input } from '@/components/ui/input'
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
import type { ActiveAI, ModelInfo } from '@/lib/types'

const modelSchema = z.object({
  model_id: z.string().min(1, '请填写上游模型 ID').max(128),
  alias: z.string().max(128).optional(),
  display_name: z.string().max(128).optional(),
  context_window: z.number().int().min(0).max(10_000_000).optional(),
})

type ModelForm = z.infer<typeof modelSchema>

const defaults: ModelForm = {
  model_id: '',
  alias: '',
  display_name: '',
  context_window: 65536,
}

export function ModelsPage() {
  const queryClient = useQueryClient()
  const [open, setOpen] = useState(false)

  const active = useQuery({
    queryKey: ['ai', 'active'],
    queryFn: () => api<{ active: ActiveAI | null }>('/admin/ai/active'),
  })

  const providerId = active.data?.active?.provider_id

  const models = useQuery({
    queryKey: ['models', providerId],
    queryFn: () =>
      api<{ items: ModelInfo[] }>(`/admin/ai/models?provider_id=${providerId ?? ''}`),
    enabled: active.isFetched,
  })

  const form = useForm<ModelForm>({
    resolver: zodResolver(modelSchema),
    defaultValues: defaults,
  })

  const create = useMutation({
    mutationFn: (values: ModelForm) =>
      api('/admin/ai/models', {
        method: 'POST',
        body: {
          provider_id: providerId,
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
      void queryClient.invalidateQueries({ queryKey: ['models'] })
    },
    onError: (error) => toast.error(errorMessage(error)),
  })

  const toggle = useMutation({
    mutationFn: (vars: { id: string; enabled: boolean }) =>
      api(`/admin/ai/models/${vars.id}`, { method: 'PATCH', body: { enabled: vars.enabled } }),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: ['models'] })
      void queryClient.invalidateQueries({ queryKey: ['ai', 'active'] })
    },
    onError: (error) => toast.error(errorMessage(error)),
  })

  const setActive = useMutation({
    mutationFn: (model: ModelInfo) =>
      api('/admin/ai/active', {
        method: 'PUT',
        body: { provider_id: model.provider_id, model_id: model.id },
      }),
    onSuccess: () => {
      toast.success('已切换当前模型，客户端下次拉取配置即生效')
      void queryClient.invalidateQueries({ queryKey: ['models'] })
      void queryClient.invalidateQueries({ queryKey: ['ai'] })
    },
    onError: (error) => toast.error(errorMessage(error)),
  })

  const remove = useMutation({
    mutationFn: (id: string) => api(`/admin/ai/models/${id}`, { method: 'DELETE' }),
    onSuccess: () => {
      toast.success('模型已删除')
      void queryClient.invalidateQueries({ queryKey: ['models'] })
    },
    onError: (error) => toast.error(errorMessage(error)),
  })

  const items = models.data?.items ?? []

  return (
    <>
      <PageHeader
        title="模型"
        description={
          active.data?.active
            ? `启用表示该模型可被调用，可以有多个；「当前」只有一个，决定客户端实际使用的模型。当前：${active.data.active.alias}`
            : '先在「供应商」中启用一个模型，这里会展示其模型清单。'
        }
        actions={
          <Dialog open={open} onOpenChange={setOpen}>
            <DialogTrigger render={<Button size="sm" disabled={!providerId} />}>
              <PlusIcon data-icon="inline-start" />
              手动添加
            </DialogTrigger>
            <DialogContent>
              <DialogHeader>
                <DialogTitle>手动添加模型</DialogTitle>
                <DialogDescription>
                  上游 /models 没有返回、但实际可用的模型可以用这里补齐。
                </DialogDescription>
              </DialogHeader>
              <form
                onSubmit={form.handleSubmit((values) => create.mutate(values))}
                className="flex flex-col gap-4"
              >
                <FieldGroup>
                  <Field data-invalid={Boolean(form.formState.errors.model_id)}>
                    <FieldLabel htmlFor="model-id">模型 ID</FieldLabel>
                    <Input
                      id="model-id"
                      placeholder="deepseek-chat"
                      aria-invalid={Boolean(form.formState.errors.model_id)}
                      {...form.register('model_id')}
                    />
                    <FieldDescription>上游请求体里的 model 字段原始值。</FieldDescription>
                    <FieldError>{form.formState.errors.model_id?.message}</FieldError>
                  </Field>
                  <Field>
                    <FieldLabel htmlFor="model-alias">别名</FieldLabel>
                    <Input id="model-alias" placeholder="留空则与模型 ID 相同" {...form.register('alias')} />
                  </Field>
                  <Field>
                    <FieldLabel htmlFor="model-display">展示名</FieldLabel>
                    <Input id="model-display" placeholder="DeepSeek Chat" {...form.register('display_name')} />
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
        }
      />

      <Card className="overflow-hidden py-0">
        <CardHeader className="px-4 pt-4">
          <CardTitle>模型清单</CardTitle>
          <CardDescription>只有启用的模型会下发给客户端</CardDescription>
        </CardHeader>
        <CardContent className="p-0">
          {models.isLoading ? (
            <div className="flex flex-col gap-2 p-4">
              {Array.from({ length: 5 }).map((_, index) => (
                <Skeleton key={index} className="h-10 w-full" />
              ))}
            </div>
          ) : items.length === 0 ? (
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
                  <TableHead className="hidden md:table-cell">别名</TableHead>
                  <TableHead className="hidden lg:table-cell text-right">上下文</TableHead>
                  <TableHead className="hidden sm:table-cell">来源</TableHead>
                  <TableHead className="text-center">启用</TableHead>
                  <TableHead className="text-center">当前</TableHead>
                  <TableHead className="text-right">操作</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {items.map((model) => (
                  <TableRow key={model.id}>
                    <TableCell>
                      <div className="flex flex-col gap-0.5">
                        <span className="font-medium">{model.display_name || model.model_id}</span>
                        <span className="font-mono text-xs text-muted-foreground">
                          {model.model_id}
                        </span>
                      </div>
                    </TableCell>
                    <TableCell className="hidden md:table-cell font-mono text-xs">
                      {model.alias}
                    </TableCell>
                    <TableCell className="hidden lg:table-cell text-right tabular-nums">
                      {model.context_window ? formatNumber(model.context_window) : '—'}
                    </TableCell>
                    <TableCell className="hidden sm:table-cell">
                      <Badge variant="outline">{model.source === 'manual' ? '手动' : '同步'}</Badge>
                    </TableCell>
                    <TableCell className="text-center">
                      <Switch
                        checked={model.enabled}
                        onCheckedChange={(checked) =>
                          toggle.mutate({ id: model.id, enabled: checked })
                        }
                        aria-label={`启用 ${model.model_id}`}
                      />
                    </TableCell>
                    <TableCell className="text-center">
                      {model.id === active.data?.active?.model_id ? (
                        <Badge>当前</Badge>
                      ) : (
                        <Button
                          variant="ghost"
                          size="xs"
                          disabled={!model.enabled || setActive.isPending}
                          onClick={() => setActive.mutate(model)}
                        >
                          设为当前
                        </Button>
                      )}
                    </TableCell>
                    <TableCell className="text-right">
                      <Button
                        variant="ghost"
                        size="icon-sm"
                        aria-label="删除模型"
                        disabled={remove.isPending}
                        onClick={() => remove.mutate(model.id)}
                      >
                        <Trash2Icon />
                      </Button>
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
