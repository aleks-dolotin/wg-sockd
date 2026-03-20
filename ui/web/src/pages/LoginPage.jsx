import { useState, useEffect, useRef } from 'react'
import { useNavigate } from 'react-router-dom'
import { useAuth } from '@/hooks/useAuth'
import { useQueryClient } from '@tanstack/react-query'
import { Card, CardContent, CardHeader, CardTitle, CardDescription } from '@/components/ui/card'
import { Input } from '@/components/ui/input'
import { Button } from '@/components/ui/button'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { toast } from 'sonner'
import { webauthnLoginBegin, webauthnLoginFinish } from '@/api/client'
import { prepareRequestOptions, serializeAssertionResponse } from '@/lib/webauthn'

export default function LoginPage() {
  const { isAuthenticated, authRequired, login, webauthnAvailable, sessionTtlSeconds } = useAuth()
  const navigate = useNavigate()
  const queryClient = useQueryClient()

  const [username, setUsername] = useState('')
  const [password, setPassword] = useState('')
  const [showPassword, setShowPassword] = useState(false)
  const [error, setError] = useState('')
  const [loading, setLoading] = useState(false)
  const [passkeyLoading, setPasskeyLoading] = useState(false)

  // AbortController ref for Conditional UI — aborted on password submit or unmount.
  const conditionalAbortRef = useRef(null)
  // Retry counter for explicit button flow.
  const passkeyRetryRef = useRef(0)

  // If already authenticated or no auth required, redirect to home.
  useEffect(() => {
    if (isAuthenticated || !authRequired) {
      navigate('/', { replace: true })
    }
  }, [isAuthenticated, authRequired, navigate])

  // Conditional UI — start when webauthnAvailable and browser supports it.
  useEffect(() => {
    if (!webauthnAvailable) return
    if (typeof PublicKeyCredential === 'undefined') return

    let cancelled = false

    async function startConditionalUI() {
      const supported = await PublicKeyCredential.isConditionalMediationAvailable?.()
      if (!supported || cancelled) return

      try {
        const beginResp = await webauthnLoginBegin()
        if (cancelled) return

        const publicKeyOptions = prepareRequestOptions(beginResp.publicKey)
        const controller = new AbortController()
        conditionalAbortRef.current = controller

        const credential = await navigator.credentials.get({
          publicKey: publicKeyOptions,
          mediation: 'conditional',
          signal: controller.signal,
        })
        if (cancelled || !credential) return

        setLoading(true)
        await webauthnLoginFinish({
          credential: serializeAssertionResponse(credential),
          token: beginResp.token,
        })
        await queryClient.invalidateQueries({ queryKey: ['auth', 'session'] })
        navigate('/', { replace: true })
      } catch (err) {
        // Conditional UI errors are silent — user can still use password.
        if (err?.name !== 'AbortError') {
          console.debug('[WebAuthn] Conditional UI error:', err)
        }
      }
    }

    startConditionalUI()
    return () => {
      cancelled = true
      conditionalAbortRef.current?.abort()
      conditionalAbortRef.current = null
    }
  }, [webauthnAvailable]) // eslint-disable-line react-hooks/exhaustive-deps

  const handleSubmit = async (e) => {
    e.preventDefault()
    // Abort Conditional UI on password submit (mutual exclusion).
    conditionalAbortRef.current?.abort()
    conditionalAbortRef.current = null

    setError('')
    setLoading(true)
    try {
      await login(username, password)
      navigate('/', { replace: true })
    } catch (err) {
      if (err.status === 401) {
        setError(err.body?.message || 'Invalid username or password')
      } else if (err.status === 429) {
        setError('Too many failed attempts. Please wait and try again.')
      } else {
        setError('An unexpected error occurred. Please try again.')
      }
    } finally {
      setLoading(false)
    }
  }

  // Explicit Passkey button — modal flow with auto-retry on first failure.
  const handlePasskeyClick = async () => {
    passkeyRetryRef.current = 0
    await runPasskeyFlow()
  }

  async function runPasskeyFlow() {
    setPasskeyLoading(true)
    try {
      const beginResp = await webauthnLoginBegin()
      const publicKeyOptions = prepareRequestOptions(beginResp.publicKey)

      const credential = await navigator.credentials.get({ publicKey: publicKeyOptions })
      if (!credential) return

      // Prevent password form submission while processing.
      setLoading(true)
      await webauthnLoginFinish({
        credential: serializeAssertionResponse(credential),
        token: beginResp.token,
      })
      await queryClient.invalidateQueries({ queryKey: ['auth', 'session'] })
      navigate('/', { replace: true })
    } catch (err) {
      if (err?.name === 'NotAllowedError') {
        toast.info('Passkey sign-in cancelled')
        return
      }
      // Auto-retry once on 400/401 (expired challenge, server restart).
      if ((err?.status === 400 || err?.status === 401) && passkeyRetryRef.current < 1) {
        passkeyRetryRef.current++
        toast.info('Passkey verification failed, trying again…')
        await runPasskeyFlow()
        return
      }
      toast.error('Passkey sign-in failed. Please use password or try again.')
    } finally {
      setPasskeyLoading(false)
      setLoading(false)
    }
  }

  const ttlMinutes = Math.round(sessionTtlSeconds / 60)

  return (
    <div className="min-h-screen flex items-center justify-center bg-background px-4">
      <Card className="w-full max-w-sm">
        <CardHeader className="text-center">
          <CardTitle className="text-xl">wg-sockd</CardTitle>
          <CardDescription>VPN Management</CardDescription>
        </CardHeader>
        <CardContent>
          <form onSubmit={handleSubmit} className="space-y-4">
            {error && (
              <Alert variant="destructive">
                <AlertDescription>{error}</AlertDescription>
              </Alert>
            )}

            <div className="space-y-2">
              <label htmlFor="username" className="text-sm font-medium leading-none">
                Username
              </label>
              <Input
                id="username"
                type="text"
                autoComplete="username webauthn"
                autoFocus
                value={username}
                onChange={(e) => setUsername(e.target.value)}
                required
              />
            </div>

            <div className="space-y-2">
              <label htmlFor="password" className="text-sm font-medium leading-none">
                Password
              </label>
              <div className="relative">
                <Input
                  id="password"
                  type={showPassword ? 'text' : 'password'}
                  autoComplete="current-password"
                  value={password}
                  onChange={(e) => setPassword(e.target.value)}
                  required
                />
                <button
                  type="button"
                  className="absolute right-2 top-1/2 -translate-y-1/2 text-muted-foreground hover:text-foreground text-xs px-1"
                  onClick={() => setShowPassword(!showPassword)}
                  tabIndex={-1}
                >
                  {showPassword ? 'Hide' : 'Show'}
                </button>
              </div>
            </div>

            <Button type="submit" className="w-full" disabled={loading || passkeyLoading}>
              {loading && !passkeyLoading ? (
                <span className="flex items-center gap-2">
                  <span className="animate-spin rounded-full h-4 w-4 border-b-2 border-current" />
                  Signing in…
                </span>
              ) : (
                'Sign In'
              )}
            </Button>

            {/* Passkey button — active when webauthnAvailable (Layer 2) */}
            {webauthnAvailable && (
              <>
                <div className="relative">
                  <div className="absolute inset-0 flex items-center">
                    <span className="w-full border-t" />
                  </div>
                  <div className="relative flex justify-center text-xs uppercase">
                    <span className="bg-background px-2 text-muted-foreground">or</span>
                  </div>
                </div>
                <Button
                  type="button"
                  variant="outline"
                  className="w-full"
                  disabled={loading || passkeyLoading}
                  onClick={handlePasskeyClick}
                >
                  {passkeyLoading ? (
                    <span className="flex items-center gap-2">
                      <span className="animate-spin rounded-full h-4 w-4 border-b-2 border-current" />
                      Verifying…
                    </span>
                  ) : (
                    'Sign in with Passkey'
                  )}
                </Button>
              </>
            )}
          </form>

          <p className="text-xs text-muted-foreground text-center mt-4">
            Session expires after {ttlMinutes} minute{ttlMinutes !== 1 ? 's' : ''}
          </p>
        </CardContent>
      </Card>
    </div>
  )
}
