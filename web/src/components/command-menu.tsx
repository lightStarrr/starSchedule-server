import { useState } from 'react'
import { useNavigate } from 'react-router'
import {
  BarChart3Icon,
  BotIcon,
  CpuIcon,
  LayoutDashboardIcon,
  MessageSquareIcon,
  SearchIcon,
  SettingsIcon,
  SmartphoneIcon,
} from 'lucide-react'
import { Button } from '@/components/ui/button'
import {
  Command,
  CommandDialog,
  CommandEmpty,
  CommandGroup,
  CommandInput,
  CommandItem,
  CommandList,
} from '@/components/ui/command'

const entries = [
  { title: '仪表盘', url: '/', icon: LayoutDashboardIcon, group: '概览' },
  { title: '设备', url: '/devices', icon: SmartphoneIcon, group: '运营' },
  { title: 'AI 用量', url: '/usage', icon: BarChart3Icon, group: '运营' },
  { title: '供应商', url: '/ai/providers', icon: BotIcon, group: 'AI' },
  { title: '模型', url: '/ai/models', icon: CpuIcon, group: 'AI' },
  { title: '试跑', url: '/ai/playground', icon: MessageSquareIcon, group: 'AI' },
  { title: '设置', url: '/settings', icon: SettingsIcon, group: '系统' },
]

export function CommandMenu() {
  const [open, setOpen] = useState(false)
  const navigate = useNavigate()

  const groups = [...new Set(entries.map((entry) => entry.group))]

  return (
    <>
      <Button
        variant="outline"
        size="sm"
        className="w-40 justify-start gap-2 text-muted-foreground md:w-56"
        onClick={() => setOpen(true)}
      >
        <SearchIcon data-icon="inline-start" />
        <span className="truncate">搜索页面</span>
        <kbd className="ml-auto hidden rounded border bg-muted px-1.5 py-0.5 text-[10px] font-medium md:inline-block">
          Ctrl K
        </kbd>
      </Button>

      <CommandDialog open={open} onOpenChange={setOpen}>
        <Command>
          <CommandInput placeholder="跳转到…" />
          <CommandList>
            <CommandEmpty>没有匹配的页面</CommandEmpty>
            {groups.map((group) => (
              <CommandGroup key={group} heading={group}>
                {entries
                  .filter((entry) => entry.group === group)
                  .map((entry) => (
                    <CommandItem
                      key={entry.url}
                      value={`${entry.title} ${entry.url}`}
                      onSelect={() => {
                        setOpen(false)
                        navigate(entry.url)
                      }}
                    >
                      <entry.icon />
                      <span>{entry.title}</span>
                    </CommandItem>
                  ))}
              </CommandGroup>
            ))}
          </CommandList>
        </Command>
      </CommandDialog>
    </>
  )
}
