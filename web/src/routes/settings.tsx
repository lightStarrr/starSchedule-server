import { useEffect } from 'react'
import { useForm, useWatch } from 'react-hook-form'
import { zodResolver } from '@hookform/resolvers/zod'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { z } from 'zod'
import { toast } from 'sonner'
import { RotateCcwIcon, SaveIcon, SettingsIcon } from 'lucide-react'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import {
  Card,
  CardContent,
  CardDescription,
  CardFooter,
  CardHeader,
  CardTitle,
} from '@/components/ui/card'
import { Field, FieldDescription, FieldError, FieldGroup, FieldLabel } from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import { Skeleton } from '@/components/ui/skeleton'
import { Spinner } from '@/components/ui/spinner'
import { Switch } from '@/components/ui/switch'
import { PageHeader } from '@/components/page-header'
import { api, errorMessage } from '@/lib/api'
import type { NumericSettingKey, SettingsPayload } from '@/lib/types'

const schema = z.object({
  quotaUnlimited: z.boolean(),
  dailyTokenQuota: z.number().int().min(1, '额度必须大于 0，不限量请开启开关'),
  rpmPerDevice: z.number().int().min(0).max(10_000),
  inflightPerDevice: z.number().int().min(0).max(100),
  globalConcurrency: z.number().int().min(0).max(10_000),
  maxOutputTokens: z.number().int().min(1).max(1_000_000),
  registerPerIPHour: z.number().int().min(0).max(100_000),
  launchRetentionDays: z.number().int().min(1).max(3650),
  aiLogRetentionDays: z.number().int().min(1).max(3650),
})

type SettingsForm = z.infer<typeof schema>

const fields: {
  name: Exclude<keyof SettingsForm, 'quotaUnlimited'>
  // 这里只承载数值型配置；ai.reasoning_effort 是字符串枚举，在模型页单独维护。
  key: NumericSettingKey
  label: string
  description: string
}[] = [
  {
    name: 'dailyTokenQuota',
    key: 'ai.daily_token_quota',
    label: '每设备每日 Token 额度',
    description: '超出后当天返回 AI_QUOTA_EXCEEDED，次日 0 点重置。',
  },
  {
    name: 'rpmPerDevice',
    key: 'ai.rpm_per_device',
    label: '每分钟请求数（单设备）',
    description: '固定窗口限流，Redis 不可用时自动放行。',
  },
  {
    name: 'inflightPerDevice',
    key: 'ai.inflight_per_device',
    label: '单设备并发上限',
    description: '同一设备同时进行的流式对话数量。',
  },
  {
    name: 'globalConcurrency',
    key: 'ai.global_concurrency',
    label: '全局并发上限',
    description: '预留给网关层，当前以单设备并发为主。',
  },
  {
    name: 'maxOutputTokens',
    key: 'ai.max_output_tokens',
    label: '单次输出上限（Token）',
    description: '客户端传更大的 max_tokens 会被服务端截断到这个值。',
  },
  {
    name: 'registerPerIPHour',
    key: 'device.register_per_ip_hour',
    label: '注册接口 IP 限额（次/小时）',
    description: '单 IP 每小时可注册的设备数，防止脚本刷量。',
  },
  {
    name: 'launchRetentionDays',
    key: 'retention.launch_event_days',
    label: '启动明细保留天数',
    description: '超过天数的 launch_events 会被定时任务清理，聚合数据保留。',
  },
  {
    name: 'aiLogRetentionDays',
    key: 'retention.ai_log_days',
    label: 'AI 日志保留天数',
    description: 'ai_request_log 只存元信息，不存对话内容。',
  },
]

export function SettingsPage() {
  const queryClient = useQueryClient()

  const settings = useQuery({
    queryKey: ['settings'],
    queryFn: () => api<SettingsPayload>('/admin/settings'),
  })

  const form = useForm<SettingsForm>({
    resolver: zodResolver(schema),
    defaultValues: {
      quotaUnlimited: false,
      dailyTokenQuota: 200000,
      rpmPerDevice: 20,
      inflightPerDevice: 2,
      globalConcurrency: 50,
      maxOutputTokens: 4096,
      registerPerIPHour: 60,
      launchRetentionDays: 90,
      aiLogRetentionDays: 30,
    },
  })
  const quotaUnlimited = useWatch({ control: form.control, name: 'quotaUnlimited' })

  useEffect(() => {
    if (!settings.data) return
    const values = settings.data.effective
    form.reset({
      quotaUnlimited: values['ai.quota_unlimited'] ?? false,
      dailyTokenQuota: values['ai.daily_token_quota'],
      rpmPerDevice: values['ai.rpm_per_device'],
      inflightPerDevice: values['ai.inflight_per_device'],
      globalConcurrency: values['ai.global_concurrency'],
      maxOutputTokens: values['ai.max_output_tokens'],
      registerPerIPHour: values['device.register_per_ip_hour'],
      launchRetentionDays: values['retention.launch_event_days'],
      aiLogRetentionDays: values['retention.ai_log_days'],
    })
  }, [settings.data, form])

  const save = useMutation({
    mutationFn: (values: SettingsForm) => {
      const payload: Record<string, number | boolean> = { 'ai.quota_unlimited': values.quotaUnlimited }
      for (const field of fields) {
        payload[field.key] = values[field.name]
      }
      return api('/admin/settings', { method: 'PUT', body: { values: payload } })
    },
    onSuccess: () => {
      toast.success('配置已保存，30 秒内对客户端生效')
      void queryClient.invalidateQueries({ queryKey: ['settings'] })
    },
    onError: (error) => toast.error(errorMessage(error)),
  })

  const resetAll = useMutation({
    mutationFn: () => {
      const payload: Record<string, null> = {}
      payload['ai.quota_unlimited'] = null
      for (const field of fields) payload[field.key] = null
      return api('/admin/settings', { method: 'PUT', body: { values: payload } })
    },
    onSuccess: () => {
      toast.success('已恢复全部默认值')
      void queryClient.invalidateQueries({ queryKey: ['settings'] })
    },
    onError: (error) => toast.error(errorMessage(error)),
  })

  const defaults = settings.data?.defaults
  const overrides = settings.data?.overrides ?? {}

  return (
    <>
      <PageHeader
        title="设置"
        description="运行时可调参数，修改后会失效缓存；客户端通过 /ai/config 读取最新限额。"
        actions={
          <Button
            variant="outline"
            size="sm"
            disabled={resetAll.isPending || settings.isLoading}
            onClick={() => resetAll.mutate()}
          >
            <RotateCcwIcon data-icon="inline-start" />
            恢复默认
          </Button>
        }
      />

      <Card>
        <CardHeader>
          <CardTitle>配额与保留策略</CardTitle>
          <CardDescription>
            默认值来自代码内置缺省，保存后写入数据库并优先于默认值。
          </CardDescription>
        </CardHeader>
        <CardContent>
          {settings.isLoading ? (
            <div className="grid gap-4 md:grid-cols-2">
              {Array.from({ length: 6 }).map((_, index) => (
                <Skeleton key={index} className="h-20 w-full" />
              ))}
            </div>
          ) : settings.isError ? (
            <p className="text-sm text-muted-foreground">加载失败：{errorMessage(settings.error)}</p>
          ) : (
            <form onSubmit={form.handleSubmit((values) => save.mutate(values))}>
              <FieldGroup className="grid gap-5 md:grid-cols-2">
                <Field className="md:col-span-2">
                  <div className="flex items-center justify-between gap-3">
                    <FieldLabel htmlFor="quota-unlimited">每日 Token 不限量</FieldLabel>
                    <Switch id="quota-unlimited" checked={quotaUnlimited}
                      onCheckedChange={(value) => form.setValue('quotaUnlimited', value, { shouldDirty: true })} />
                  </div>
                  <FieldDescription>开启后不限制每日 Token 用量；请求频率、并发和单次输出限制仍然生效。</FieldDescription>
                </Field>
                {fields.map((field) => {
                  const error = form.formState.errors[field.name]
                  const overridden = overrides[field.key] !== undefined
                  return (
                    <Field key={field.key} data-invalid={Boolean(error)}>
                      <div className="flex items-center justify-between gap-2">
                        <FieldLabel htmlFor={field.key}>{field.label}</FieldLabel>
                        {overridden ? <Badge variant="outline">已覆盖</Badge> : null}
                      </div>
                      <Input
                        id={field.key}
                        type="number"
                        min={field.name === 'dailyTokenQuota' ? 1 : 0}
                        readOnly={field.name === 'dailyTokenQuota' && quotaUnlimited}
                        aria-disabled={field.name === 'dailyTokenQuota' && quotaUnlimited}
                        className={field.name === 'dailyTokenQuota' && quotaUnlimited ? 'pointer-events-none opacity-50' : undefined}
                        aria-invalid={Boolean(error)}
                        {...form.register(field.name, { valueAsNumber: true })}
                      />
                      <FieldDescription>
                        默认 {defaults?.[field.key] ?? '—'} · {field.description}
                      </FieldDescription>
                      <FieldError>{error?.message}</FieldError>
                    </Field>
                  )
                })}
              </FieldGroup>

              <div className="mt-6 flex items-center gap-2">
                <Button type="submit" disabled={save.isPending}>
                  {save.isPending ? <Spinner data-icon="inline-start" /> : <SaveIcon data-icon="inline-start" />}
                  保存配置
                </Button>
                <span className="text-xs text-muted-foreground">
                  保存后 30 秒内生效（Redis 缓存 TTL）
                </span>
              </div>
            </form>
          )}
        </CardContent>
        <CardFooter className="gap-2 text-xs text-muted-foreground">
          <SettingsIcon className="size-3.5" aria-hidden />
          这些参数只影响新请求，不会改变历史统计数据。
        </CardFooter>
      </Card>
    </>
  )
}
