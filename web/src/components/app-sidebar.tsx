import { NavLink, useLocation } from 'react-router'
import {
  BarChart3Icon,
  BotIcon,
  CpuIcon,
  LayoutDashboardIcon,
  MessageSquareIcon,
  SettingsIcon,
  SmartphoneIcon,
} from 'lucide-react'
import {
  Sidebar,
  SidebarContent,
  SidebarGroup,
  SidebarGroupContent,
  SidebarGroupLabel,
  SidebarHeader,
  SidebarMenu,
  SidebarMenuButton,
  SidebarMenuItem,
  SidebarRail,
} from '@/components/ui/sidebar'

const groups = [
  {
    label: '概览',
    items: [{ title: '仪表盘', url: '/', icon: LayoutDashboardIcon }],
  },
  {
    label: '运营',
    items: [
      { title: '设备', url: '/devices', icon: SmartphoneIcon },
      { title: 'AI 用量', url: '/usage', icon: BarChart3Icon },
    ],
  },
  {
    label: 'AI',
    items: [
      { title: '供应商', url: '/ai/providers', icon: BotIcon },
      { title: '模型', url: '/ai/models', icon: CpuIcon },
      { title: '试跑', url: '/ai/playground', icon: MessageSquareIcon },
    ],
  },
  {
    label: '系统',
    items: [{ title: '设置', url: '/settings', icon: SettingsIcon }],
  },
]

export function AppSidebar() {
  const { pathname } = useLocation()

  return (
    <Sidebar collapsible="icon">
      <SidebarHeader>
        <SidebarMenu>
          <SidebarMenuItem>
            <SidebarMenuButton size="lg" render={<NavLink to="/" />}>
              <img
                src={`${import.meta.env.BASE_URL}app-icon-light.svg`}
                alt="星课表"
                className="size-8 shrink-0 rounded-md dark:hidden"
              />
              <img
                src={`${import.meta.env.BASE_URL}app-icon-dark.svg`}
                alt=""
                className="hidden size-8 shrink-0 rounded-md dark:block"
              />
              <div className="flex flex-col gap-0.5 leading-none group-data-[collapsible=icon]:hidden">
                <span className="font-medium">星课表</span>
                <span className="text-xs text-muted-foreground">管理后台</span>
              </div>
            </SidebarMenuButton>
          </SidebarMenuItem>
        </SidebarMenu>
      </SidebarHeader>

      <SidebarContent>
        {groups.map((group) => (
          <SidebarGroup key={group.label}>
            <SidebarGroupLabel>{group.label}</SidebarGroupLabel>
            <SidebarGroupContent>
              <SidebarMenu>
                {group.items.map((item) => {
                  const active =
                    item.url === '/' ? pathname === '/' : pathname.startsWith(item.url)
                  return (
                    <SidebarMenuItem key={item.url}>
                      <SidebarMenuButton
                        isActive={active}
                        tooltip={item.title}
                        render={<NavLink to={item.url} />}
                      >
                        <item.icon />
                        <span>{item.title}</span>
                      </SidebarMenuButton>
                    </SidebarMenuItem>
                  )
                })}
              </SidebarMenu>
            </SidebarGroupContent>
          </SidebarGroup>
        ))}
      </SidebarContent>

      <SidebarRail />
    </Sidebar>
  )
}
