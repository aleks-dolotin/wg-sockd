import { useQuery } from '@tanstack/react-query'
import {
  fetchPeers,
  fetchPeer,
  fetchProfiles,
  fetchStats,
  fetchConnectionStatus,
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
    refetchInterval: 15_000, // stats refresh more frequently
  })
}

export function useConnectionStatus() {
  return useQuery({
    queryKey: ['connectionStatus'],
    queryFn: fetchConnectionStatus,
    refetchInterval: 5_000, // match Go proxy health-check interval
    retry: false,
  })
}

