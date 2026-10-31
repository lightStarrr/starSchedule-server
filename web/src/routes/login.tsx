import { useEffect, useState } from 'react'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { Navigate, useLocation } from 'react-router'
import { SparklesIcon } from 'lucide-react'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Field, FieldGroup, FieldLabel } from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import { Spinner } from '@/components/ui/spinner'
import { api, ApiError, errorMessage } from '@/lib/api'
import type { AdminProfile } from '@/lib/types'

export function LoginPage() {
  const location = useLocation()
  const queryClient = useQueryClient()
  const [username, setUsername] = useState('')
  const [password, setPassword] = useState('')
  const [error, setError] = useState<string | null>(null)
  const [retryAfter, setRetryAfter] = useState(0)
  const [done, setDone] = useState(false)

  const login = useMutation({
    mutationFn: () =>
      api<{ token: string; admin: AdminProfile }>('/admin/login', {
        method: 'POST',
        body: { username: username.trim(), password },
      }),
    onSuccess: () => {
      // 清掉登录失败期间缓存的 401，避免外壳组件直接弹回登录页
      void queryClient.invalidateQueries({ queryKey: ['admin', 'me'] })
      setDone(true)
    },
    onError: (err) => {
      setError(errorMessage(err))
      if (err instanceof ApiError && err.code === 'RATE_LIMITED') {
        const wait = Number(err.details?.retry_after ?? 0)
        setRetryAfter(Number.isFinite(wait) && wait > 0 ? wait : 60)
      }
    },
  })

  useEffect(() => {
    if (retryAfter <= 0) return
    const timer = window.setInterval(() => {
      setRetryAfter((value) => (value <= 1 ? 0 : value - 1))
    }, 1000)
    return () => window.clearInterval(timer)
  }, [retryAfter])

  if (done) {
    const from = (location.state as { from?: string } | null)?.from
    return <Navigate to={from && from !== '/login' ? from : '/'} replace />
  }

  return (
    <div className="grid min-h-svh place-items-center bg-background p-4">
      <div
        aria-hidden
        className="pointer-events-none absolute inset-0 bg-[radial-gradient(60rem_40rem_at_50%_-10%,var(--muted),transparent)]"
      />
      <Card className="relative w-full max-w-sm">
        <CardHeader>
          <div className="mb-2 flex size-9 items-center justify-center rounded-lg bg-primary text-primary-foreground">
            <SparklesIcon />
          </div>
          <CardTitle>登录管理后台</CardTitle>
          <CardDescription>星课表运营与 AI 配置控制台</CardDescription>
        </CardHeader>
        <CardContent>
          <form
            onSubmit={(event) => {
              event.preventDefault()
              setError(null)
              login.mutate()
            }}
            className="flex flex-col gap-4"
          >
            <FieldGroup>
              <Field>
                <FieldLabel htmlFor="username">用户名</FieldLabel>
                <Input
                  id="username"
                  autoComplete="username"
                  value={username}
                  onChange={(event) => setUsername(event.target.value)}
                  required
                />
              </Field>
              <Field>
                <FieldLabel htmlFor="password">密码</FieldLabel>
                <Input
                  id="password"
                  type="password"
                  autoComplete="current-password"
                  value={password}
                  onChange={(event) => setPassword(event.target.value)}
                  required
                />
              </Field>
            </FieldGroup>

            {error ? (
              <Alert variant="destructive">
                <AlertTitle>登录失败</AlertTitle>
                <AlertDescription>
                  {retryAfter > 0 ? `尝试过于频繁，请 ${retryAfter} 秒后重试。` : error}
                </AlertDescription>
              </Alert>
            ) : null}

            <Button type="submit" disabled={login.isPending || retryAfter > 0}>
              {login.isPending ? <Spinner data-icon="inline-start" /> : null}
              {login.isPending ? '登录中…' : '登录'}
            </Button>
          </form>
        </CardContent>
      </Card>
    </div>
  )
}
