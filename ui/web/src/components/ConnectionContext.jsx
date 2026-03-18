import { createContext, useContext } from 'react'
import { useConnectionStatus } from '@/api/hooks'

const ConnectionContext = createContext({ state: 'unknown', isConnected: false, version: '', commit: '' })

export function ConnectionProvider({ children }) {
  const { data } = useConnectionStatus()
  const state = data?.state || 'unknown'
  const isConnected = state === 'connected'
  const version = data?.version || ''
  const commit = data?.commit || ''

  return (
    <ConnectionContext.Provider value={{ state, isConnected, version, commit }}>
      {children}
    </ConnectionContext.Provider>
  )
}

export function useConnection() {
  return useContext(ConnectionContext)
}

