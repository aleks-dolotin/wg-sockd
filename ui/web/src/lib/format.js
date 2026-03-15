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
 * Validate a CIDR string (basic client-side check).
 */
export function isValidCIDR(cidr) {
  const re = /^(\d{1,3}\.){3}\d{1,3}\/\d{1,2}$/
  if (!re.test(cidr)) return false
  const [ip, prefix] = cidr.split('/')
  const parts = ip.split('.').map(Number)
  return parts.every(p => p >= 0 && p <= 255) && Number(prefix) >= 0 && Number(prefix) <= 32
}

