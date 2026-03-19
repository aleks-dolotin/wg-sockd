/**
 * Convert a profile API response into ProfileForm state.
 */
export function profileToFormData(p) {
  return {
    name: p.name,
    allowed_ips: (p.allowed_ips || []).join(', '),
    exclude_ips: (p.exclude_ips || []).join(', '),
    description: p.description || '',
    persistent_keepalive: p.persistent_keepalive != null ? String(p.persistent_keepalive) : '',
    client_dns: p.client_dns || '',
    client_mtu: p.client_mtu != null ? String(p.client_mtu) : '',
    client_allowed_ips: p.client_allowed_ips || '',
    use_preshared_key: p.use_preshared_key ?? false,
  }
}

/**
 * Convert ProfileForm state into API payload.
 */
export function formDataToPayload(data) {
  const payload = {
    ...data,
    allowed_ips: data.allowed_ips.split(',').map(s => s.trim()).filter(Boolean),
    exclude_ips: data.exclude_ips ? data.exclude_ips.split(',').map(s => s.trim()).filter(Boolean) : [],
  }
  if (data.persistent_keepalive !== '') payload.persistent_keepalive = parseInt(data.persistent_keepalive, 10)
  else delete payload.persistent_keepalive
  if (data.client_mtu !== '') payload.client_mtu = parseInt(data.client_mtu, 10)
  else delete payload.client_mtu
  return payload
}

