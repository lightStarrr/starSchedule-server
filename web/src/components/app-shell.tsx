import { useQuery, useQueryClient } from '@tanstack/react-query'
import { useState } from 'react'
import { Link, Navigate, Outlet, useLocation, useNavigate } from 'react-router'
import { LogOutIcon } from 'lucide-react'
import { Button } from '@/components/ui/button'
import {
  Breadcrumb,
  BreadcrumbItem,
  BreadcrumbList,
  BreadcrumbPage,
  BreadcrumbSeparator,
} from '@/components/ui/breadcrumb'
import { Separator } from '@/components/ui/separator'
import { SidebarInset, SidebarProvider, SidebarTrigger } from '@/components/ui/sidebar'
import { Skeleton } from '@/components/ui/skeleton'
import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from '@/components/ui/tooltip'
import { AppSidebar } from '@/components/app-sidebar'
import { CommandMenu } from '@/components/command-menu'
import { ThemeToggle } from '@/components/theme-toggle'
import { api, ApiError, errorMessage } from '@/lib/api'
import type { AdminProfile } from '@/lib/types'

const titles: Record<string, string> = {
  '/': '仪表盘',
  '/devices': '设备',
  '/ai/providers': 'AI 供应商',
  '/ai/models': '模型',
  '/ai/playground': 'AI 试跑',
  '/usage': 'AI 用量',
  '/settings': '设置',
}

function currentTitle(pathname: string): string {
  if (pathname.startsWith('/devices/')) return '设备详情'
  return titles[pathname] ?? '管理后台'
}

export function AppShell() {
  const location = useLocation()
  const navigate = useNavigate()
  const queryClient = useQueryClient()
  const [loggingOut, setLoggingOut] = useState(false)

  const me = useQuery({
    queryKey: ['admin', 'me'],
    queryFn: () => api<AdminProfile>('/admin/me'),
    retry: false,
    staleTime: 60_000,
  })

  if (me.isLoading) {
    return (
      <div className="flex min-h-svh">
        <div className="hidden w-64 border-r p-4 md:block">
          <Skeleton className="h-9 w-full" />
        </div>
        <div className="flex flex-1 flex-col gap-4 p-6">
          <Skeleton className="h-8 w-48" />
          <Skeleton className="h-40 w-full" />
          <Skeleton className="h-64 w-full" />
        </div>
      </div>
    )
  }

  if (me.error) {
    const status = me.error instanceof ApiError ? me.error.status : 0
    if (status === 401 || status === 403) {
      return <Navigate to="/login" replace state={{ from: location.pathname }} />
    }
    return (
      <div className="grid min-h-svh place-items-center p-6">
        <div className="flex max-w-sm flex-col items-center gap-3 text-center">
          <p className="font-heading text-lg">无法连接服务端</p>
          <p className="text-sm text-muted-foreground">{errorMessage(me.error)}</p>
          <Button variant="outline" size="sm" onClick={() => void me.refetch()}>
            重试
          </Button>
        </div>
      </div>
    )
  }

  const handleLogout = async () => {
    setLoggingOut(true)
    try {
      await api('/admin/logout', { method: 'POST' })
    } catch {
      /* 会话可能已过期，忽略 */
    } finally {
      queryClient.clear()
      setLoggingOut(false)
      navigate('/login', { replace: true })
    }
  }

  return (
    <TooltipProvider>
      <SidebarProvider>
        <AppSidebar />
        <SidebarInset>
          <header className="flex h-14 shrink-0 items-center gap-2 border-b px-4">
            <SidebarTrigger className="-ml-1" />
            {/* 手机上顶栏空间紧张，分隔线只在 md 以上保留 */}
            <Separator
              orientation="vertical"
              className="mr-1 hidden data-[orientation=vertical]:h-4 md:block"
            />
            <Breadcrumb>
              <BreadcrumbList>
                <BreadcrumbItem className="hidden md:block">
                  <Link
                    to="/"
                    className="text-muted-foreground transition-colors hover:text-foreground"
                  >
                    星课表
                  </Link>
                </BreadcrumbItem>
                <BreadcrumbSeparator className="hidden md:block" />
                <BreadcrumbItem>
                  <BreadcrumbPage>{currentTitle(location.pathname)}</BreadcrumbPage>
                </BreadcrumbItem>
              </BreadcrumbList>
            </Breadcrumb>

            <div className="ml-auto flex items-center gap-2">
              <CommandMenu />
              <ThemeToggle />
              <Tooltip>
                <TooltipTrigger
                  render={
                    <Button
                      variant="ghost"
                      size="icon-sm"
                      aria-label="退出登录"
                      disabled={loggingOut}
                      onClick={handleLogout}
                    >
                      <LogOutIcon />
                    </Button>
                  }
                />
                <TooltipContent>退出登录</TooltipContent>
              </Tooltip>
            </div>
          </header>

          <div className="flex flex-1 flex-col gap-6 p-4 md:p-6">
            <Outlet />
          </div>
        </SidebarInset>
      </SidebarProvider>
    </TooltipProvider>
  )
}
