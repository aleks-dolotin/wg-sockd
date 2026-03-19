import { BrowserRouter, Routes, Route } from 'react-router-dom'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { ConnectionProvider } from '@/components/ConnectionContext'
import AuthGuard from '@/components/AuthGuard'
import Layout from '@/components/Layout'
import LoginPage from '@/pages/LoginPage'
import PeersPage from '@/pages/PeersPage'
import PeerNewPage from '@/pages/PeerNewPage'
import PeerDetailPage from '@/pages/PeerDetailPage'
import ProfilesPage from '@/pages/ProfilesPage'
import ProfileNewPage from '@/pages/ProfileNewPage'
import ProfileDetailPage from '@/pages/ProfileDetailPage'
import SettingsPage from '@/pages/SettingsPage'
import Dashboard from '@/pages/Dashboard'

const queryClient = new QueryClient({
  defaultOptions: {
    queries: {
      staleTime: 10_000,        // 10s — data considered fresh
      refetchInterval: 30_000,  // 30s — background refetch
      retry: 1,
    },
  },
})

export default function App() {
  return (
    <QueryClientProvider client={queryClient}>
      <ConnectionProvider>
        <BrowserRouter>
          <Routes>
            {/* Login page — outside AuthGuard */}
            <Route path="/login" element={<LoginPage />} />
            {/* Protected routes — inside AuthGuard */}
            <Route element={<AuthGuard />}>
              <Route element={<Layout />}>
                <Route path="/" element={<Dashboard />} />
                <Route path="/peers" element={<PeersPage />} />
                <Route path="/peers/new" element={<PeerNewPage />} />
                <Route path="/peers/:id" element={<PeerDetailPage />} />
                <Route path="/settings/profiles" element={<ProfilesPage />} />
                <Route path="/settings/profiles/new" element={<ProfileNewPage />} />
                <Route path="/settings/profiles/:name" element={<ProfileDetailPage />} />
                <Route path="/settings" element={<SettingsPage />} />
              </Route>
            </Route>
          </Routes>
        </BrowserRouter>
      </ConnectionProvider>
    </QueryClientProvider>
  )
}
