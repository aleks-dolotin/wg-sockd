import { useEffect } from 'react'

export function usePageTitle(title) {
  useEffect(() => {
    document.title = title ? `${title} — wg-sockd` : 'wg-sockd'
    return () => { document.title = 'wg-sockd' }
  }, [title])
}
