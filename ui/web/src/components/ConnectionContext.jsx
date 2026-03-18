import { createContext, useContext, useState, useEffect } from 'react'
import { useConnectionStatus } from '@/api/hooks'
import { fetchConnectionStatus } from '@/api/client'

const ConnectionContext = createContext({ state: 'unknown', isConnected: false, version: '', commit: '' })

export function ConnectionProvider({ children }) {
  const { data } = useConnectionStatus()
  const state = data?.state || 'unknown'
  const isConnected = state === 'connected'
  const [version, setVersion] = useState('')
  const [commit, setCommit] = useState('')

  // Fetch version info from /ui/status (Go proxy endpoint, separate from agent health).
  useEffect(() => {
    fetchConnectionStatus()
      .then(info => {
        if (info?.version) setVersion(info.version)
        if (info?.commit) setCommit(info.commit)
      })
      .catch(() => {}) // silently ignore — version display is best-effort
  }, [])

  return (
    <ConnectionContext.Provider value={{ state, isConnected, version, commit }}>
      {children}
    </ConnectionContext.Provider>
  )
}

export function useConnection() {
  return useContext(ConnectionContext)
}

