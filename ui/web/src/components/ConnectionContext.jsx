import { createContext, useContext } from 'react'
import { useConnectionStatus } from '@/api/hooks'

const ConnectionContext = createContext({ state: 'unknown', isConnected: false })

export function ConnectionProvider({ children }) {
  const { data } = useConnectionStatus()
  const state = data?.state || 'unknown'
  const isConnected = state === 'connected'

  return (
    <ConnectionContext.Provider value={{ state, isConnected }}>
      {children}
    </ConnectionContext.Provider>
  )
}

export function useConnection() {
  return useContext(ConnectionContext)
}

