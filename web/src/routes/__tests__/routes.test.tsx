import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest'
import { render, screen } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { createMemoryRouter, RouterProvider } from 'react-router'
import { ThemeProvider } from 'next-themes'
import { routes } from '@/router'

/**
 * 渲染冒烟测试：每个页面都真实渲染一次。
 * 目的是抓住「构建通过但运行时缺 Provider / 字段不匹配」这类错误，
 * 例如之前漏掉 SidebarProvider 导致登录后整页白屏。
 */

function json(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json' },
  })
}

const today = {
  dau: 12,
  launches: 30,
  new_devices: 3,
  ai_requests: 5,
  ai_tokens: 900,
}

const emptyDistribution = { items: [{ key: '1.0.0', count: 7 }] }
const trend = {
  items: [{ date: '2026-10-30', dau: 10, launches: 20, new_devices: 1, ai_requests: 2, ai_tokens: 100 }],
}

const provider = {
  id: 'p1',
  name: 'deepseek',
  base_url: 'https://api.deepseek.com/v1',
  api_key_masked: '****abcd',
  enabled: true,
  sort_order: 0,
  timeout_seconds: 120,
  created_at: '2026-10-31T00:00:00Z',
}

const model = {
  id: 'm1',
  provider_id: 'p1',
  provider_name: 'deepseek',
  model_id: 'deepseek-chat',
  alias: 'deepseek-chat',
  display_name: 'DeepSeek Chat',
  context_window: 65536,
  source: 'fetched' as const,
  enabled: true,
  fetched_at: '2026-10-31T00:00:00Z',
}

const catalog = {
  items: [
    {
      provider_id: 'p1',
      provider_name: 'deepseek',
      id: 'm1',
      model_id: 'deepseek-chat',
      alias: 'deepseek-chat',
      display_name: 'DeepSeek Chat',
      context_window: 65536,
      enabled: true,
      source: 'fetched',
      fetched_at: '2026-10-31T00:00:00Z',
    },
  ],
  active: { id: 'm1', alias: 'deepseek-chat' },
}

const device = {
  id: 'd1',
  install_id: 'install-1234',
  platform: 'android',
  app_version: '1.0.0',
  version_code: 1,
  os_version: '14',
  sdk_int: 34,
  arch: 'arm64-v8a',
  status: 'active' as const,
  first_seen_at: '2026-10-01T00:00:00Z',
  last_seen_at: '2026-10-31T00:00:00Z',
  launch_count: 42,
  last_ip: '1.1.1.1',
  suspect_reinstall: false,
}

const settings = {
  overrides: { 'ai.rpm_per_device': 10 },
  effective: {
    'ai.daily_token_quota': 200000,
    'ai.rpm_per_device': 10,
    'ai.inflight_per_device': 2,
    'ai.global_concurrency': 50,
    'ai.max_output_tokens': 4096,
    'device.register_per_ip_hour': 60,
    'retention.launch_event_days': 90,
    'retention.ai_log_days': 30,
  },
  defaults: {
    'ai.daily_token_quota': 200000,
    'ai.rpm_per_device': 20,
    'ai.inflight_per_device': 2,
    'ai.global_concurrency': 50,
    'ai.max_output_tokens': 4096,
    'device.register_per_ip_hour': 60,
    'retention.launch_event_days': 90,
    'retention.ai_log_days': 30,
  },
}

/** 按路径返回固定数据，未覆盖的接口返回空对象。 */
function mockApi() {
  const fetchMock = vi.fn(async (input: RequestInfo | URL) => {
    const url = typeof input === 'string' ? input : input.toString()
    const path = url.replace(/^https?:\/\/[^/]+/, '')

    if (path.startsWith('/api/v1/admin/me')) return json({ id: 'a1', username: 'admin', role: 'admin' })
    if (path.startsWith('/api/v1/admin/stats/overview'))
      return json({ today, yesterday: today, wau: 30, mau: 90, date: '2026-10-31' })
    if (path.startsWith('/api/v1/admin/stats/trend')) return json(trend)
    if (path.startsWith('/api/v1/admin/stats/distribution')) return json(emptyDistribution)
    if (path.startsWith('/api/v1/admin/ai/catalog')) return json(catalog)
    if (path.startsWith('/api/v1/admin/ai/active'))
      return json({ active: { provider_id: 'p1', provider_name: 'deepseek', model_id: 'm1', model: 'deepseek-chat', alias: 'deepseek-chat', display_name: 'DeepSeek Chat', context_window: 65536 } })
    if (path.startsWith('/api/v1/admin/ai/providers')) return json({ items: [provider] })
    if (path.startsWith('/api/v1/admin/ai/models')) return json({ items: [model] })
    if (path.startsWith('/api/v1/admin/ai/usage'))
      return json({ group_by: 'day', items: [{ date: '2026-10-30', request_count: 3, tokens: 600, errors: 0 }] })
    if (path.startsWith('/api/v1/admin/settings')) return json(settings)
    if (path.startsWith('/api/v1/admin/devices/'))
      return json({
        device,
        activity: [{ stat_date: '2026-10-30', launch_count: 5 }],
        launches: [
          {
            id: 'l1',
            device_id: 'd1',
            stat_date: '2026-10-30',
            occurred_at: '2026-10-30T08:00:00Z',
            received_at: '2026-10-30T08:00:01Z',
            launch_type: 'cold',
            platform: 'android',
            app_version: '1.0.0',
            os_version: '14',
            arch: 'arm64-v8a',
            session_id: 's1',
          },
        ],
        usage_today: { request_count: 2, prompt_tokens: 100, completion_tokens: 200, tokens: 300, estimated_count: 0 },
      })
    if (path.startsWith('/api/v1/admin/devices'))
      return json({ items: [device], next_cursor: '', has_more: false, total_estimate: 1 })
    return json({})
  })

  vi.stubGlobal('fetch', fetchMock)
  return fetchMock
}

function renderAt(path: string) {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false, gcTime: 0 } },
  })
  const router = createMemoryRouter(routes, { initialEntries: [path] })
  return render(
    <ThemeProvider attribute="class" defaultTheme="dark" enableSystem={false}>
      <QueryClientProvider client={queryClient}>
        <RouterProvider router={router} />
      </QueryClientProvider>
    </ThemeProvider>,
  )
}

beforeEach(() => {
  mockApi()
})

afterEach(() => {
  vi.unstubAllGlobals()
})

describe('管理后台页面渲染', () => {
  const cases: { path: string; heading: string }[] = [
    { path: '/', heading: '仪表盘' },
    { path: '/devices', heading: '设备' },
    { path: '/devices/d1', heading: '设备 install-…' },
    { path: '/ai/providers', heading: 'AI 供应商' },
    { path: '/ai/models', heading: '模型' },
    { path: '/ai/playground', heading: 'AI 试跑' },
    { path: '/usage', heading: 'AI 用量' },
    { path: '/settings', heading: '设置' },
  ]

  for (const c of cases) {
    it(`${c.path} 能正常渲染`, async () => {
      renderAt(c.path)
      expect(await screen.findByRole('heading', { level: 1, name: c.heading })).toBeInTheDocument()
    })
  }

  it('外壳渲染出侧边栏导航（回归：缺少 SidebarProvider 会整页白屏）', async () => {
    renderAt('/')
    // 面包屑的当前页也是 role=link，因此用 findAll
    expect((await screen.findAllByRole('link', { name: /仪表盘/ })).length).toBeGreaterThan(0)
    const links = screen.getAllByRole('link').map((el) => el.textContent ?? '')
    for (const name of ['仪表盘', '设备', '供应商', '模型', '试跑', '设置']) {
      expect(links.some((text) => text.includes(name))).toBe(true)
    }
  })

  it('试跑页模型下拉显示名称与供应商，而不是 ID', async () => {
    renderAt('/ai/playground')
    expect(await screen.findByText('DeepSeek Chat（deepseek）')).toBeInTheDocument()
  })

  it('模型页展示供应商与模型 ID', async () => {
    renderAt('/ai/models')
    expect(await screen.findByText('DeepSeek Chat')).toBeInTheDocument()
    expect(screen.getAllByText('deepseek').length).toBeGreaterThan(0)
  })
})
