import { useQuery, useQueryClient } from '@tanstack/react-query'
import { useCallback } from 'react'
import { fetchSession, login as apiLogin, logout as apiLogout } from '@/api/client'

/**
 * useAuth hook — manages authentication state via GET /api/auth/session.
 *
 * Returns:
 * - user: { username, expires_at } or null
 * - isLoading: session check in progress
 * - isAuthenticated: has valid session
 * - authRequired: server has auth enabled (false = backward compat, no login needed)
 * - webauthnAvailable: passkeys registered (Layer 2)
 * - sessionTtlSeconds: TTL from server (F10: not hardcoded)
 * - login(username, password): async, invalidates session query on success
 * - logout(): async, invalidates + returns
 * - checkSession(): refetch session
 */
export function useAuth() {
  const queryClient = useQueryClient()

  const { data, isLoading, refetch } = useQuery({
    queryKey: ['auth', 'session'],
    queryFn: async () => {
      try {
        return await fetchSession()
      } catch (err) {
        // 401 is expected when not authenticated — return the body.
        if (err.status === 401 && err.body) {
          return { ...err.body, _unauthorized: true }
        }
        throw err
      }
    },
    retry: false,
    staleTime: 30_000,
    refetchOnWindowFocus: true,
  })

  const authRequired = data?.auth_required ?? true
  const isAuthenticated = !isLoading && !!data?.username && !data?._unauthorized
  const webauthnAvailable = data?.webauthn_available ?? false
  const sessionTtlSeconds = data?.session_ttl_seconds ?? 900

  const user = isAuthenticated ? { username: data.username, expires_at: data.expires_at } : null

  const login = useCallback(async (username, password) => {
    const result = await apiLogin(username, password)
    await queryClient.invalidateQueries({ queryKey: ['auth', 'session'] })
    return result
  }, [queryClient])

  const logout = useCallback(async () => {
    await apiLogout()
    queryClient.setQueryData(['auth', 'session'], null)
    await queryClient.invalidateQueries({ queryKey: ['auth', 'session'] })
  }, [queryClient])

  return {
    user,
    isLoading,
    isAuthenticated,
    authRequired,
    webauthnAvailable,
    sessionTtlSeconds,
    login,
    logout,
    checkSession: refetch,
  }
}
