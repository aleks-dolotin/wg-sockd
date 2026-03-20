import { useSearchParams } from 'react-router-dom'
import { useCallback, useRef, useEffect, useState } from 'react'
import { isPeerOnline } from '@/lib/format'

export function usePeerFilters() {
  const [searchParams, setSearchParams] = useSearchParams()

  const query = searchParams.get('q') || ''
  const statusFilter = searchParams.get('status') || 'all'
  const profileFilter = searchParams.get('profile') || 'all'
  const autoFilter = searchParams.get('filter') === 'auto_discovered' ? 'yes' : (searchParams.get('auto') || 'all')
  const sortField = searchParams.get('sort') || 'tunnel_address'
  const sortDir = searchParams.get('dir') || 'asc'

  // Debounced search input value
  const [debouncedQuery, setDebouncedQuery] = useState(query)
  const timerRef = useRef(null)

  const setParam = useCallback((key, value, defaults = {}) => {
    setSearchParams(prev => {
      const next = new URLSearchParams(prev)
      // Clear legacy 'filter' param when setting new filters
      if (key !== 'filter') next.delete('filter')
      if (value === (defaults[key] || 'all') || value === '') {
        next.delete(key)
      } else {
        next.set(key, value)
      }
      return next
    }, { replace: true })
  }, [setSearchParams])

  const setQuery = useCallback((val) => {
    setDebouncedQuery(val)
    if (timerRef.current) clearTimeout(timerRef.current)
    timerRef.current = setTimeout(() => {
      setParam('q', val)
    }, 300)
  }, [setParam])

  useEffect(() => () => { if (timerRef.current) clearTimeout(timerRef.current) }, [])

  const setStatusFilter = useCallback((val) => setParam('status', val), [setParam])
  const setProfileFilter = useCallback((val) => setParam('profile', val), [setParam])
  const setAutoFilter = useCallback((val) => setParam('auto', val), [setParam])

  const toggleSort = useCallback((field) => {
    setSearchParams(prev => {
      const next = new URLSearchParams(prev)
      next.delete('filter')
      const currentField = prev.get('sort') || 'tunnel_address'
      const currentDir = prev.get('dir') || 'asc'
      if (currentField === field) {
        next.set('dir', currentDir === 'asc' ? 'desc' : 'asc')
      } else {
        next.set('sort', field)
        next.set('dir', 'asc')
      }
      return next
    }, { replace: true })
  }, [setSearchParams])

  const filterAndSort = useCallback((peers) => {
    if (!peers) return []
    let result = [...peers]

    // Search
    const q = (query || '').toLowerCase()
    if (q) {
      result = result.filter(p =>
        (p.friendly_name || '').toLowerCase().includes(q) ||
        (p.public_key || '').toLowerCase().includes(q)
      )
    }

    // Status filter
    if (statusFilter === 'online') {
      result = result.filter(p => isPeerOnline(p.latest_handshake))
    } else if (statusFilter === 'offline') {
      result = result.filter(p => !isPeerOnline(p.latest_handshake))
    } else if (statusFilter === 'disabled') {
      result = result.filter(p => !p.enabled)
    }

    // Profile filter
    if (profileFilter !== 'all') {
      if (profileFilter === '__none__') {
        result = result.filter(p => !p.profile)
      } else {
        result = result.filter(p => p.profile === profileFilter)
      }
    }

    // Auto-discovered filter
    if (autoFilter === 'yes') {
      result = result.filter(p => p.auto_discovered)
    } else if (autoFilter === 'no') {
      result = result.filter(p => !p.auto_discovered)
    }

    // Sort
    const parseIp = (addr) => {
      if (!addr) return 0
      const ip = addr.split('/')[0]
      const parts = ip.split('.')
      return parts.reduce((acc, octet) => (acc << 8) + parseInt(octet, 10), 0) >>> 0
    }

    result.sort((a, b) => {
      let cmp = 0
      switch (sortField) {
        case 'tunnel_address':
          cmp = parseIp(a.client_address) - parseIp(b.client_address)
          break
        case 'name':
          cmp = (a.friendly_name || '').localeCompare(b.friendly_name || '')
          break
        case 'status': {
          const aOnline = isPeerOnline(a.latest_handshake) ? 1 : 0
          const bOnline = isPeerOnline(b.latest_handshake) ? 1 : 0
          cmp = bOnline - aOnline
          break
        }
        case 'transfer':
          cmp = ((b.transfer_rx || 0) + (b.transfer_tx || 0)) - ((a.transfer_rx || 0) + (a.transfer_tx || 0))
          break
        case 'profile':
          cmp = (a.profile || '').localeCompare(b.profile || '')
          break
        default:
          cmp = 0
      }
      return sortDir === 'desc' ? -cmp : cmp
    })

    return result
  }, [query, statusFilter, profileFilter, autoFilter, sortField, sortDir])

  return {
    query: debouncedQuery,
    statusFilter,
    profileFilter,
    autoFilter,
    sortField,
    sortDir,
    setQuery,
    setStatusFilter,
    setProfileFilter,
    setAutoFilter,
    toggleSort,
    filterAndSort,
  }
}
