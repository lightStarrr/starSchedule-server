interface Usage {
  prompt_tokens: number
  completion_tokens: number
  total_tokens: number
  estimated: boolean
}

export interface ChatMessage {
  role: 'system' | 'user' | 'assistant'
  content: string
}

export interface StreamHandlers {
  onMeta?: (meta: { id: string; model: string; alias: string; request_id: string }) => void
  onDelta?: (text: string) => void
  onUsage?: (usage: Usage) => void
  onError?: (error: { code: string; message: string }) => void
  onDone?: () => void
}

interface StreamBody {
  messages: ChatMessage[]
  temperature?: number
  max_tokens?: number
  /** 指定模型 alias；不传则用后台「当前启用」的模型 */
  model?: string
  /** 思考强度；不传则用该模型配置的默认档位 */
  reasoning_effort?: string
}

/** 用 fetch 读 SSE（EventSource 不支持 POST，也无法带 Cookie 之外的鉴权头）。 */
export async function streamPlayground(
  body: StreamBody,
  handlers: StreamHandlers,
  signal?: AbortSignal,
): Promise<void> {
  const res = await fetch('/api/v1/admin/ai/playground', {
    method: 'POST',
    credentials: 'same-origin',
    signal,
    headers: {
      'Content-Type': 'application/json',
      Accept: 'text/event-stream',
      'X-Requested-With': 'XMLHttpRequest',
    },
    body: JSON.stringify({ ...body, stream: true }),
  })

  if (!res.ok || !res.body) {
    let message = `请求失败（HTTP ${res.status}）`
    let code = 'INTERNAL'
    try {
      const data = (await res.json()) as { error?: { message?: string; code?: string } }
      if (data.error?.message) message = data.error.message
      if (data.error?.code) code = data.error.code
    } catch {
      /* 忽略解析失败 */
    }
    handlers.onError?.({ code, message })
    return
  }

  const reader = res.body.getReader()
  const decoder = new TextDecoder()
  let buffer = ''
  let event = 'message'
  let dataLines: string[] = []
  let finished = false

  const dispatch = () => {
    if (dataLines.length === 0) return
    const payload = dataLines.join('\n')
    dataLines = []

    if (event === 'error') {
      try {
        const parsed = JSON.parse(payload) as { error?: { code?: string; message?: string } }
        handlers.onError?.({
          code: parsed.error?.code ?? 'UPSTREAM_ERROR',
          message: parsed.error?.message ?? '生成中断',
        })
      } catch {
        handlers.onError?.({ code: 'UPSTREAM_ERROR', message: payload })
      }
      finished = true
      return
    }

    if (payload === '[DONE]') {
      finished = true
      handlers.onDone?.()
      return
    }

    try {
      const chunk = JSON.parse(payload) as {
        choices?: { delta?: { content?: string } }[]
        usage?: Usage
      }
      if (event === 'meta') {
        handlers.onMeta?.(JSON.parse(payload))
      }
      const delta = chunk.choices?.[0]?.delta?.content
      if (delta) handlers.onDelta?.(delta)
      if (chunk.usage) handlers.onUsage?.(chunk.usage)
    } catch {
      /* 非 JSON 帧忽略 */
    }
  }

  while (!finished) {
    const { done, value } = await reader.read()
    if (done) break
    buffer += decoder.decode(value, { stream: true })

    let index = buffer.indexOf('\n')
    while (index >= 0) {
      const rawLine = buffer.slice(0, index)
      buffer = buffer.slice(index + 1)
      const line = rawLine.endsWith('\r') ? rawLine.slice(0, -1) : rawLine

      if (line === '') {
        dispatch()
        event = 'message'
      } else if (line.startsWith(':')) {
        // 注释帧（心跳）
      } else if (line.startsWith('event:')) {
        event = line.slice(6).trim()
      } else if (line.startsWith('data:')) {
        dataLines.push(line.slice(5).replace(/^ /, ''))
      }

      if (finished) break
      index = buffer.indexOf('\n')
    }
  }

  if (!finished) handlers.onDone?.()
}
