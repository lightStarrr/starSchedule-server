export class ApiError extends Error {
  readonly status: number
  readonly code: string
  readonly requestId: string
  readonly details?: Record<string, unknown>

  constructor(
    status: number,
    code: string,
    message: string,
    requestId = '',
    details?: Record<string, unknown>,
  ) {
    super(message)
    this.name = 'ApiError'
    this.status = status
    this.code = code
    this.requestId = requestId
    this.details = details
  }
}

type RequestOptions = Omit<RequestInit, 'body'> & { body?: unknown }

/** 统一的接口调用：同源 Cookie 会话 + JSON 错误归一。 */
export async function api<T>(path: string, options: RequestOptions = {}): Promise<T> {
  const { body, headers, ...rest } = options
  const res = await fetch(`/api/v1${path}`, {
    credentials: 'same-origin',
    ...rest,
    headers: {
      'X-Requested-With': 'XMLHttpRequest',
      ...(body !== undefined ? { 'Content-Type': 'application/json' } : {}),
      ...headers,
    },
    body: body === undefined ? undefined : JSON.stringify(body),
  })

  if (res.status === 204) {
    return undefined as T
  }

  const text = await res.text()
  let payload: unknown = null
  if (text) {
    try {
      payload = JSON.parse(text)
    } catch {
      payload = null
    }
  }

  if (!res.ok) {
    const err = (payload as { error?: Record<string, unknown> } | null)?.error
    throw new ApiError(
      res.status,
      typeof err?.code === 'string' ? err.code : 'INTERNAL',
      typeof err?.message === 'string' ? err.message : `请求失败（HTTP ${res.status}）`,
      typeof err?.request_id === 'string' ? err.request_id : '',
      (err?.details as Record<string, unknown> | undefined) ?? undefined,
    )
  }

  return payload as T
}

/** 把错误转成可直接展示的中文文案。 */
export function errorMessage(error: unknown): string {
  if (error instanceof ApiError) {
    return error.requestId ? `${error.message}（${error.requestId.slice(0, 8)}）` : error.message
  }
  if (error instanceof Error) return error.message
  return '未知错误'
}
