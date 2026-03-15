// API client — all requests go to relative /api/ (proxy handles routing).

const BASE = '/api'

class ApiError extends Error {
  constructor(status, body) {
    super(body?.message || `API error ${status}`)
    this.status = status
    this.body = body
  }
}

async function request(path, options = {}) {
  const url = `${BASE}${path}`
  const res = await fetch(url, {
    headers: { 'Content-Type': 'application/json', ...options.headers },
    ...options,
  })
  if (!res.ok) {
    let body
    try { body = await res.json() } catch { body = null }
    throw new ApiError(res.status, body)
  }
  if (res.status === 204) return null
  return res.json()
}

// --- Peers ---
export const fetchPeers = () => request('/peers')
export const fetchPeer = (id) => request(`/peers/${id}`)
export const createPeer = (data) => request('/peers', { method: 'POST', body: JSON.stringify(data) })
export const updatePeer = (id, data) => request(`/peers/${id}`, { method: 'PUT', body: JSON.stringify(data) })
export const deletePeer = (id) => request(`/peers/${id}`, { method: 'DELETE' })
export const rotatePeerKeys = (id) => request(`/peers/${id}/rotate-keys`, { method: 'POST' })
export const approvePeer = (id) => request(`/peers/${id}/approve`, { method: 'POST' })
export const batchCreatePeers = (peers) => request('/peers/batch', { method: 'POST', body: JSON.stringify({ peers }) })

// --- Profiles ---
export const fetchProfiles = () => request('/profiles')
export const fetchProfile = (name) => request(`/profiles/${name}`)
export const createProfile = (data) => request('/profiles', { method: 'POST', body: JSON.stringify(data) })
export const updateProfile = (name, data) => request(`/profiles/${name}`, { method: 'PUT', body: JSON.stringify(data) })
export const deleteProfile = (name) => request(`/profiles/${name}`, { method: 'DELETE' })

// --- Stats ---
export const fetchStats = () => request('/stats')

// --- Health ---
export const fetchHealth = () => request('/health')

// --- Connection status (from Go proxy, not agent) ---
export const fetchConnectionStatus = () =>
  fetch('/ui/status').then(r => r.json())

