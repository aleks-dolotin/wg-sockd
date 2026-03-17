/**
 * Format bytes into human-readable string (KB/MB/GB/TB).
 */
export function formatBytes(bytes) {
  if (bytes == null || bytes === 0) return '0 B'
  const units = ['B', 'KB', 'MB', 'GB', 'TB']
  const i = Math.min(Math.floor(Math.log(bytes) / Math.log(1024)), units.length - 1)
  return `${(bytes / Math.pow(1024, i)).toFixed(1)} ${units[i]}`
}

/**
 * Truncate a WireGuard public key for display: first 8 + "…" + last 4.
 */
export function truncateKey(key) {
  if (!key || key.length < 16) return key || ''
  return `${key.slice(0, 8)}…${key.slice(-4)}`
}

/**
 * Check if a peer is "online" based on last handshake (< 3 minutes ago).
 */
export function isPeerOnline(latestHandshake) {
  if (!latestHandshake) return false
  const threshold = 3 * 60 * 1000 // 3 minutes
  return Date.now() - new Date(latestHandshake).getTime() < threshold
}

/**
 * Format a timestamp as relative time ("2 min ago", "3 hours ago").
 */
export function formatRelativeTime(timestamp) {
  if (!timestamp) return 'never'
  const diff = Date.now() - new Date(timestamp).getTime()
  if (diff < 0) return 'just now'
  const seconds = Math.floor(diff / 1000)
  if (seconds < 60) return `${seconds}s ago`
  const minutes = Math.floor(seconds / 60)
  if (minutes < 60) return `${minutes} min ago`
  const hours = Math.floor(minutes / 60)
  if (hours < 24) return `${hours}h ago`
  const days = Math.floor(hours / 24)
  return `${days}d ago`
}

/**
 * Validate a CIDR string — supports both IPv4 and IPv6.
 */
export function isValidCIDR(cidr) {
  if (!cidr || typeof cidr !== 'string') return false
  const slash = cidr.lastIndexOf('/')
  if (slash === -1) return false

  const ip = cidr.slice(0, slash)
  const prefixStr = cidr.slice(slash + 1)
  if (!/^\d{1,3}$/.test(prefixStr)) return false
  const prefix = Number(prefixStr)

  // IPv4 check
  const ipv4Re = /^(\d{1,3}\.){3}\d{1,3}$/
  if (ipv4Re.test(ip)) {
    const parts = ip.split('.').map(Number)
    return parts.every(p => p >= 0 && p <= 255) && prefix >= 0 && prefix <= 32
  }

  // IPv6 check — must contain at least one colon
  if (!ip.includes(':')) return false
  if (prefix < 0 || prefix > 128) return false

  // Expand :: shorthand for validation
  const parts = ip.split('::')
  if (parts.length > 2) return false // only one :: allowed

  const left = parts[0] ? parts[0].split(':') : []
  const right = parts.length === 2 && parts[1] ? parts[1].split(':') : []
  const totalGroups = left.length + right.length

  if (parts.length === 2) {
    // With :: — total groups must be ≤ 7 (:: expands to fill up to 8)
    if (totalGroups > 7) return false
  } else {
    // Without :: — must be exactly 8 groups
    if (totalGroups !== 8) return false
  }

  const hexGroupRe = /^[0-9a-fA-F]{1,4}$/
  return [...left, ...right].every(g => hexGroupRe.test(g))
}
