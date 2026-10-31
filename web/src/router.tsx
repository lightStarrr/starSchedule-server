import { createBrowserRouter, type RouteObject } from 'react-router'
import { AppShell } from '@/components/app-shell'
import { Skeleton } from '@/components/ui/skeleton'

// 懒加载路由首次匹配时需要一个占位，否则首帧渲染空节点
function RouteFallback() {
  return (
    <div className="flex flex-col gap-4 p-6">
      <Skeleton className="h-8 w-48" />
      <Skeleton className="h-40 w-full" />
    </div>
  )
}

/** 生成懒加载路由，统一挂上占位组件。 */
function page(load: () => Promise<Record<string, unknown>>, key: string) {
  return async () => ({
    Component: (await load())[key] as never,
    HydrateFallback: RouteFallback,
  })
}

// 路由级懒加载：图表库只在需要时下载，首屏更轻。
// 单独导出便于测试用 createMemoryRouter 复用同一份配置。
export const routes: RouteObject[] = [
  {
    path: '/login',
    lazy: page(() => import('@/routes/login'), 'LoginPage'),
  },
  {
    path: '/',
    element: <AppShell />,
    children: [
      { index: true, lazy: page(() => import('@/routes/dashboard'), 'DashboardPage') },
      { path: 'devices', lazy: page(() => import('@/routes/devices'), 'DevicesPage') },
      { path: 'devices/:id', lazy: page(() => import('@/routes/device-detail'), 'DeviceDetailPage') },
      { path: 'ai/providers', lazy: page(() => import('@/routes/providers'), 'ProvidersPage') },
      { path: 'ai/models', lazy: page(() => import('@/routes/models'), 'ModelsPage') },
      { path: 'ai/playground', lazy: page(() => import('@/routes/playground'), 'PlaygroundPage') },
      { path: 'usage', lazy: page(() => import('@/routes/usage'), 'UsagePage') },
      { path: 'settings', lazy: page(() => import('@/routes/settings'), 'SettingsPage') },
    ],
  },
]

export const router = createBrowserRouter(routes, { basename: '/admin' })
