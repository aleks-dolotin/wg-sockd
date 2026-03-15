import { BrowserRouter, Routes, Route } from 'react-router-dom'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { ConnectionProvider } from '@/components/ConnectionContext'
import Layout from '@/components/Layout'
import PeersPage from '@/pages/PeersPage'
import PeerNewPage from '@/pages/PeerNewPage'
import PeerDetailPage from '@/pages/PeerDetailPage'
import ProfilesPage from '@/pages/ProfilesPage'
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
            <Route element={<Layout />}>
              <Route path="/" element={<PeersPage />} />
              <Route path="/peers/new" element={<PeerNewPage />} />
              <Route path="/peers/:id" element={<PeerDetailPage />} />
              <Route path="/dashboard" element={<Dashboard />} />
              <Route path="/settings/profiles" element={<ProfilesPage />} />
            </Route>
          </Routes>
        </BrowserRouter>
      </ConnectionProvider>
    </QueryClientProvider>
  )
}
