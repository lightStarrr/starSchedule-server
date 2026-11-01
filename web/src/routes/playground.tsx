import { useEffect, useRef, useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { Link } from 'react-router'
import { SendIcon, SquareIcon, Trash2Icon, WandSparklesIcon } from 'lucide-react'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from '@/components/ui/card'
import { Field, FieldDescription, FieldGroup, FieldLabel } from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import { ScrollArea } from '@/components/ui/scroll-area'
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { Separator } from '@/components/ui/separator'
import { Textarea } from '@/components/ui/textarea'
import { PageHeader } from '@/components/page-header'
import { api } from '@/lib/api'
import { formatNumber } from '@/lib/format'
import { streamPlayground, type ChatMessage } from '@/lib/stream'
import type { CatalogResponse } from '@/lib/types'

interface Usage {
  prompt_tokens: number
  completion_tokens: number
  total_tokens: number
  estimated: boolean
}

export function PlaygroundPage() {
  const catalog = useQuery({
    queryKey: ['ai', 'catalog'],
    queryFn: () => api<CatalogResponse>('/admin/ai/catalog'),
  })

  const items = catalog.data?.items ?? []
  const [modelKey, setModelKey] = useState('')

  // 默认选后台「当前启用」的模型，没有则取第一个可用的
  useEffect(() => {
    if (modelKey) return
    const preferred = catalog.data?.active.id
    if (preferred && items.some((item) => item.id === preferred)) {
      setModelKey(preferred)
    } else if (items.length > 0) {
      setModelKey(items[0].id)
    }
  }, [catalog.data, items, modelKey])

  const [messages, setMessages] = useState<ChatMessage[]>([])
  const [input, setInput] = useState('')
  const [draft, setDraft] = useState('')
  const [streaming, setStreaming] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [usage, setUsage] = useState<Usage | null>(null)
  const [firstTokenMs, setFirstTokenMs] = useState<number | null>(null)
  const [elapsedMs, setElapsedMs] = useState<number | null>(null)
  const [temperature, setTemperature] = useState(0.7)
  const [maxTokens, setMaxTokens] = useState(1024)

  const abortRef = useRef<AbortController | null>(null)
  const bottomRef = useRef<HTMLDivElement | null>(null)

  useEffect(() => {
    bottomRef.current?.scrollIntoView({ behavior: 'smooth' })
  }, [messages, draft])

  useEffect(() => () => abortRef.current?.abort(), [])

  const send = async () => {
    const content = input.trim()
    if (!content || streaming) return

    const nextMessages: ChatMessage[] = [...messages, { role: 'user', content }]
    setMessages(nextMessages)
    setInput('')
    setDraft('')
    setError(null)
    setUsage(null)
    setFirstTokenMs(null)
    setElapsedMs(null)
    setStreaming(true)

    const controller = new AbortController()
    abortRef.current = controller
    const started = performance.now()
    let text = ''

    try {
      await streamPlayground(
        { messages: nextMessages, temperature, max_tokens: maxTokens, model: modelKey },
        {
          onDelta: (delta) => {
            text += delta
            setDraft(text)
            setFirstTokenMs((value) => value ?? Math.round(performance.now() - started))
          },
          onUsage: (value) => setUsage(value),
          onError: (err) => setError(`${err.message}（${err.code}）`),
        },
        controller.signal,
      )
    } catch (err) {
      if (!controller.signal.aborted) {
        setError(err instanceof Error ? err.message : '请求失败')
      }
    } finally {
      setElapsedMs(Math.round(performance.now() - started))
      setStreaming(false)
      abortRef.current = null
      if (text) {
        setMessages((prev) => [...prev, { role: 'assistant', content: text }])
      }
      setDraft('')
    }
  }

  const stop = () => {
    abortRef.current?.abort()
  }

  // 模型被删除/停用后，下拉选中项可能失效，自动回落到第一个可用模型
  useEffect(() => {
    if (modelKey && !items.some((item) => item.id === modelKey)) {
      setModelKey(items[0]?.id ?? '')
    }
  }, [items, modelKey])


  return (
    <>
      <PageHeader
        title="AI 试跑"
        description="选定模型直接打对应供应商，用来验证密钥、模型与流式链路。不占用设备配额。"
      />

      {items.length === 0 && !catalog.isLoading ? (
        <Alert>
          <WandSparklesIcon />
          <AlertTitle>还没有可用的模型</AlertTitle>
          <AlertDescription>
            先到
            <Link to="/ai/providers" className="mx-1 underline underline-offset-4">
              供应商
            </Link>
            添加上游并启用模型，然后回到这里试跑。
          </AlertDescription>
        </Alert>
      ) : null}

      <div className="grid gap-4 lg:grid-cols-[minmax(0,1fr)_300px]">
        <Card className="flex min-h-[520px] flex-col">
          <CardHeader>
            <CardTitle>对话</CardTitle>
            <CardDescription>流式输出，可随时中断</CardDescription>
          </CardHeader>
          <CardContent className="flex flex-1 flex-col gap-3">
            <ScrollArea className="h-[320px] rounded-lg border bg-muted/20 p-3">
              {messages.length === 0 && !draft ? (
                <p className="py-10 text-center text-sm text-muted-foreground">
                  输入一句话试试，例如「帮我把周三的高数挪到周五」。
                </p>
              ) : (
                <div className="flex flex-col gap-4">
                  {messages.map((message, index) => (
                    <div key={index} className="flex flex-col gap-1">
                      <span className="text-xs font-medium text-muted-foreground">
                        {message.role === 'user' ? '你' : '助手'}
                      </span>
                      <p className="whitespace-pre-wrap text-sm leading-relaxed">{message.content}</p>
                    </div>
                  ))}
                  {draft ? (
                    <div className="flex flex-col gap-1">
                      <span className="text-xs font-medium text-muted-foreground">助手</span>
                      <p className="whitespace-pre-wrap text-sm leading-relaxed">{draft}</p>
                    </div>
                  ) : null}
                  <div ref={bottomRef} />
                </div>
              )}
            </ScrollArea>

            {error ? (
              <Alert variant="destructive">
                <AlertTitle>生成失败</AlertTitle>
                <AlertDescription>{error}</AlertDescription>
              </Alert>
            ) : null}

            <div className="flex flex-col gap-2">
              <Textarea
                value={input}
                onChange={(event) => setInput(event.target.value)}
                placeholder="输入消息，Ctrl + Enter 发送"
                className="min-h-20 resize-none"
                onKeyDown={(event) => {
                  if ((event.metaKey || event.ctrlKey) && event.key === 'Enter') {
                    event.preventDefault()
                    void send()
                  }
                }}
              />
              <div className="flex items-center gap-2">
                <Button size="sm" onClick={() => void send()} disabled={streaming || !input.trim()}>
                  <SendIcon data-icon="inline-start" />
                  发送
                </Button>
                <Button variant="outline" size="sm" onClick={stop} disabled={!streaming}>
                  <SquareIcon data-icon="inline-start" />
                  中断
                </Button>
                <Button
                  variant="ghost"
                  size="sm"
                  className="ml-auto"
                  onClick={() => {
                    setMessages([])
                    setDraft('')
                    setUsage(null)
                    setFirstTokenMs(null)
                    setElapsedMs(null)
                    setError(null)
                  }}
                  disabled={streaming}
                >
                  <Trash2Icon data-icon="inline-start" />
                  清空
                </Button>
              </div>
            </div>
          </CardContent>
        </Card>

        <div className="flex flex-col gap-4">
          <Card size="sm">
            <CardHeader>
              <CardTitle>本次指标</CardTitle>
            </CardHeader>
            <CardContent className="flex flex-col gap-2 text-sm">
              <div className="flex items-center justify-between">
                <span className="text-muted-foreground">首字延迟</span>
                <span className="tabular-nums">
                  {firstTokenMs === null ? '—' : `${firstTokenMs} ms`}
                </span>
              </div>
              <div className="flex items-center justify-between">
                <span className="text-muted-foreground">总耗时</span>
                <span className="tabular-nums">{elapsedMs === null ? '—' : `${elapsedMs} ms`}</span>
              </div>
              <Separator />
              <div className="flex items-center justify-between">
                <span className="text-muted-foreground">输入 tokens</span>
                <span className="tabular-nums">{formatNumber(usage?.prompt_tokens)}</span>
              </div>
              <div className="flex items-center justify-between">
                <span className="text-muted-foreground">输出 tokens</span>
                <span className="tabular-nums">{formatNumber(usage?.completion_tokens)}</span>
              </div>
              <div className="flex items-center justify-between">
                <span className="text-muted-foreground">总计</span>
                <div className="flex items-center gap-2">
                  {usage?.estimated ? <Badge variant="outline">估算</Badge> : null}
                  <span className="tabular-nums">{formatNumber(usage?.total_tokens)}</span>
                </div>
              </div>
            </CardContent>
          </Card>

          <Card size="sm">
            <CardHeader>
              <CardTitle>参数</CardTitle>
              <CardDescription>仅影响本次试跑</CardDescription>
            </CardHeader>
            <CardContent>
              <FieldGroup>
                <Field>
                  <FieldLabel>模型</FieldLabel>
                  <Select
                    value={modelKey}
                    onValueChange={(value) => setModelKey(value ?? '')}
                    items={items.map((item) => ({
                      value: item.id,
                      label: `${item.display_name}（${item.provider_name}）`,
                    }))}
                  >
                    <SelectTrigger className="w-full">
                      <SelectValue placeholder="选择模型" />
                    </SelectTrigger>
                    <SelectContent>
                      <SelectGroup>
                        {items.map((item) => (
                          <SelectItem key={item.id} value={item.id}>
                            {item.display_name}（{item.provider_name}）
                          </SelectItem>
                        ))}
                      </SelectGroup>
                    </SelectContent>
                  </Select>
                  <FieldDescription>
                    仅影响本次试跑；客户端默认使用
                    {catalog.data?.active.alias || '未设置'}
                  </FieldDescription>
                </Field>

                <Field>
                  <FieldLabel htmlFor="temperature">temperature</FieldLabel>
                  <Input
                    id="temperature"
                    type="number"
                    min={0}
                    max={2}
                    step={0.1}
                    value={temperature}
                    onChange={(event) => setTemperature(Number(event.target.value))}
                  />
                </Field>
                <Field>
                  <FieldLabel htmlFor="max-tokens">max_tokens</FieldLabel>
                  <Input
                    id="max-tokens"
                    type="number"
                    min={1}
                    value={maxTokens}
                    onChange={(event) => setMaxTokens(Number(event.target.value))}
                  />
                  <FieldDescription>服务端仍会按后台上限截断。</FieldDescription>
                </Field>
              </FieldGroup>
            </CardContent>
          </Card>
        </div>
      </div>
    </>
  )
}
