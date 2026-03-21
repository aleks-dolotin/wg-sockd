import { Outlet, NavLink } from 'react-router-dom'
import { useState } from 'react'
import ConnectionStatus from '@/components/ConnectionStatus'
import UnknownPeerAlert from '@/components/UnknownPeerAlert'
import StaleDataBanner from '@/components/StaleDataBanner'
import { Toaster } from '@/components/ui/sonner'
import { useDarkMode } from '@/hooks/useDarkMode'
import { useConnection } from '@/components/ConnectionContext'

const navItems = [
  { to: '/', label: 'Dashboard' },
  { to: '/peers', label: 'Peers' },
  { to: '/settings/profiles', label: 'Profiles' },
  { to: '/settings', label: 'Settings' },
]

export default function Layout() {
  const [menuOpen, setMenuOpen] = useState(false)
  const { isDark, toggle } = useDarkMode()
  const { version, commit } = useConnection()

  return (
    <div className="min-h-screen flex flex-col">
      {/* Header */}
      <header className="border-b bg-background sticky top-0 z-10">
        <div className="max-w-5xl mx-auto px-4 h-14 flex items-center justify-between">
          <div className="flex items-center gap-4">
            <h1 className="text-lg font-semibold tracking-tight">wg-sockd</h1>
            {/* Desktop nav */}
            <nav className="hidden md:flex items-center gap-1">
              {navItems.map(item => (
                <NavLink
                  key={item.to}
                  to={item.to}
                  end={item.to === '/' || item.to === '/settings'}
                  className={({ isActive }) =>
                    `px-3 py-1.5 rounded-md text-sm transition-colors ${
                      isActive
                        ? 'bg-muted text-foreground font-medium'
                        : 'text-muted-foreground hover:text-foreground hover:bg-muted/50'
                    }`
                  }
                >
                  {item.label}
                </NavLink>
              ))}
            </nav>
          </div>

          <div className="flex items-center gap-3">
            <ConnectionStatus />
            {/* Dark mode toggle */}
            <button
              onClick={toggle}
              className="p-2 rounded-md hover:bg-muted text-muted-foreground hover:text-foreground transition-colors"
              aria-label={isDark ? 'Switch to light mode' : 'Switch to dark mode'}
            >
              {isDark ? (
                <svg className="w-4 h-4" fill="none" viewBox="0 0 24 24" stroke="currentColor" strokeWidth={2}>
                  <path strokeLinecap="round" strokeLinejoin="round" d="M12 3v1m0 16v1m9-9h-1M4 12H3m15.364 6.364l-.707-.707M6.343 6.343l-.707-.707m12.728 0l-.707.707M6.343 17.657l-.707.707M16 12a4 4 0 11-8 0 4 4 0 018 0z" />
                </svg>
              ) : (
                <svg className="w-4 h-4" fill="none" viewBox="0 0 24 24" stroke="currentColor" strokeWidth={2}>
                  <path strokeLinecap="round" strokeLinejoin="round" d="M20.354 15.354A9 9 0 018.646 3.646 9.003 9.003 0 0012 21a9.003 9.003 0 008.354-5.646z" />
                </svg>
              )}
            </button>
            {/* Mobile hamburger */}
            <button
              className="md:hidden p-2 rounded-md hover:bg-muted"
              onClick={() => setMenuOpen(!menuOpen)}
              aria-label="Toggle menu"
            >
              <svg className="w-5 h-5" fill="none" viewBox="0 0 24 24" stroke="currentColor">
                {menuOpen ? (
                  <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M6 18L18 6M6 6l12 12" />
                ) : (
                  <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M4 6h16M4 12h16M4 18h16" />
                )}
              </svg>
            </button>
          </div>
        </div>

        {/* Mobile nav */}
        {menuOpen && (
          <nav className="md:hidden border-t px-4 py-2 space-y-1">
            {navItems.map(item => (
              <NavLink
                key={item.to}
                to={item.to}
                end={item.to === '/'}
                onClick={() => setMenuOpen(false)}
                className={({ isActive }) =>
                  `block px-3 py-2 rounded-md text-sm ${
                    isActive
                      ? 'bg-muted text-foreground font-medium'
                      : 'text-muted-foreground hover:text-foreground hover:bg-muted/50'
                  }`
                }
              >
                {item.label}
              </NavLink>
            ))}
          </nav>
        )}
      </header>

      <StaleDataBanner />

      {/* Main content */}
      <main className="flex-1 max-w-5xl mx-auto w-full px-4 py-6">
        <UnknownPeerAlert />
        <Outlet />
      </main>

      {/* Footer */}
      {version && version !== 'dev' && (
        <footer className="border-t py-3 text-center text-xs text-muted-foreground">
          wg-sockd-ui v{version}{commit && commit !== 'unknown' ? ` (${commit.slice(0, 8)})` : ''}
        </footer>
      )}

      <Toaster theme={isDark ? 'dark' : 'light'} />
    </div>
  )
}
