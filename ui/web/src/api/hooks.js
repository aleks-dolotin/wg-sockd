import { useQuery } from '@tanstack/react-query'
import {
  fetchPeers,
  fetchPeer,
  fetchProfiles,
  fetchStats,
  fetchHealth,
} from './client'

export function usePeers() {
  return useQuery({
    queryKey: ['peers'],
    queryFn: fetchPeers,
  })
}

export function usePeer(id) {
  return useQuery({
    queryKey: ['peers', id],
    queryFn: () => fetchPeer(id),
    enabled: !!id,
  })
}

export function useProfiles() {
  return useQuery({
    queryKey: ['profiles'],
    queryFn: fetchProfiles,
  })
}

export function useStats() {
  return useQuery({
    queryKey: ['stats'],
    queryFn: fetchStats,
    refetchInterval: 15_000,
  })
}

export function useConnectionStatus() {
  return useQuery({
    queryKey: ['connectionStatus'],
    queryFn: async () => {
      // Use /api/health as the source of truth for connection status.
      // Works in all modes: dev, embedded UI, and behind Go proxy.
      const health = await fetchHealth()
      return { state: health.status === 'ok' || health.status === 'degraded' ? 'connected' : 'disconnected' }
    },
    refetchInterval: 5_000,
    retry: false,
  })
}

