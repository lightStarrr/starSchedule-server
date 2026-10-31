import { createBrowserRouter } from 'react-router'
import { AppShell } from '@/components/app-shell'

// 路由级懒加载：图表库只在需要时下载，首屏更轻。
export const router = createBrowserRouter(
  [
    {
      path: '/login',
      lazy: async () => ({ Component: (await import('@/routes/login')).LoginPage }),
    },
    {
      path: '/',
      element: <AppShell />,
      children: [
        {
          index: true,
          lazy: async () => ({ Component: (await import('@/routes/dashboard')).DashboardPage }),
        },
        {
          path: 'devices',
          lazy: async () => ({ Component: (await import('@/routes/devices')).DevicesPage }),
        },
        {
          path: 'devices/:id',
          lazy: async () => ({
            Component: (await import('@/routes/device-detail')).DeviceDetailPage,
          }),
        },
        {
          path: 'ai/providers',
          lazy: async () => ({ Component: (await import('@/routes/providers')).ProvidersPage }),
        },
        {
          path: 'ai/models',
          lazy: async () => ({ Component: (await import('@/routes/models')).ModelsPage }),
        },
        {
          path: 'ai/playground',
          lazy: async () => ({ Component: (await import('@/routes/playground')).PlaygroundPage }),
        },
        {
          path: 'usage',
          lazy: async () => ({ Component: (await import('@/routes/usage')).UsagePage }),
        },
        {
          path: 'settings',
          lazy: async () => ({ Component: (await import('@/routes/settings')).SettingsPage }),
        },
      ],
    },
  ],
  { basename: '/admin' },
)
