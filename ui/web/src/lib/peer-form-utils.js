/**
 * Convert a peer API response into PeerForm state.
 */
export function peerToFormData(peer) {
  return {
    name: peer.friendly_name || '',
    profile: peer.profile || '',
    allowedIPs: (peer.allowed_ips || []).join(', '),
    notes: peer.notes || '',
    clientAddress: peer.client_address || '',
    endpoint: peer.configured_endpoint || '',
    pka: peer.persistent_keepalive != null ? String(peer.persistent_keepalive) : '',
    clientDNS: peer.client_dns || '',
    clientMTU: peer.client_mtu != null ? String(peer.client_mtu) : '',
    clientAllowedIPs: peer.client_allowed_ips || '',
    generatePSK: false,
  }
}

