import { createContext, useContext } from 'react'

export const AuthContext = createContext(null)

/**
 * useAuthContext — access user info and logout from any child component.
 */
export function useAuthContext() {
  return useContext(AuthContext)
}
