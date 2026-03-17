import { useEffect } from 'react'
import { useNavigate, Outlet } from 'react-router-dom'
import { useAuth } from '@/hooks/useAuth'
import { AuthContext } from '@/components/AuthContext'

/**
 * AuthGuard — wraps protected routes.
 *
 * - isLoading → full-page spinner
 * - !authRequired → render children (no auth mode, backward compat)
 * - isAuthenticated → render children with AuthContext
 * - !isAuthenticated → redirect to /login
 * - Re-checks session on window focus
 */
export default function AuthGuard() {
  const { user, isLoading, isAuthenticated, authRequired, logout, checkSession } = useAuth()
  const navigate = useNavigate()

  // Re-check session on window focus.
  useEffect(() => {
    const onFocus = () => checkSession()
    window.addEventListener('focus', onFocus)
    return () => window.removeEventListener('focus', onFocus)
  }, [checkSession])

  // Redirect to login if not authenticated.
  useEffect(() => {
    if (!isLoading && authRequired && !isAuthenticated) {
      navigate('/login', { replace: true })
    }
  }, [isLoading, authRequired, isAuthenticated, navigate])

  if (isLoading) {
    return (
      <div className="min-h-screen flex items-center justify-center">
        <div className="animate-spin rounded-full h-8 w-8 border-b-2 border-foreground" />
      </div>
    )
  }

  // No auth required — backward compat.
  if (!authRequired) {
    return (
      <AuthContext.Provider value={{ user: null, logout: () => {} }}>
        <Outlet />
      </AuthContext.Provider>
    )
  }

  if (!isAuthenticated) {
    // Will redirect via useEffect above.
    return null
  }

  return (
    <AuthContext.Provider value={{ user, logout }}>
      <Outlet />
    </AuthContext.Provider>
  )
}
