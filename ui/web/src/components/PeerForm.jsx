import { useState, useEffect } from 'react'
import { useProfiles } from '@/api/hooks'
import { useConnection } from '@/components/ConnectionContext'
import { fetchNextAddress } from '@/api/client'
import { TooltipProvider } from '@/components/ui/tooltip'
import FieldLabel from '@/components/FieldLabel'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Badge } from '@/components/ui/badge'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { isValidCIDR } from '@/lib/format'
import { peerToFormData } from '@/lib/peer-form-utils'
import { toast } from 'sonner'


const emptyForm = {
  name: '', profile: '', notes: '',
  clientAddress: '', endpoint: '', pka: '',
  clientDNS: '', clientMTU: '', clientAllowedIPs: '',
  generatePSK: false,
}

export default function PeerForm({ initialData, mode = 'create', peer, onSubmit, isPending, error, onCancel }) {
  const [form, setForm] = useState(initialData || emptyForm)
  const [cidrError, setCidrError] = useState('')
  const [endpointError, setEndpointError] = useState('')
  const [clientAddressError, setClientAddressError] = useState('')

  const { data: profiles } = useProfiles()
  const { isConnected } = useConnection()
  const hasProfiles = profiles && profiles.length > 0
  const selectedProfile = profiles?.find(p => p.name === form.profile)

  // Auto-fill tunnel address on create.
  useEffect(() => {
    if (mode !== 'create' || form.clientAddress) return

    let ignore = false
    fetchNextAddress()
      .then(data => {
        if (!ignore && data?.next_address) {
          setForm(f => f.clientAddress ? f : { ...f, clientAddress: data.next_address })
        }
      })
      .catch(() => {}) // silently ignore — user can fill manually

    return () => { ignore = true }
  }, [mode]) // eslint-disable-line react-hooks/exhaustive-deps

  function applyProfile(profileName) {
    const prof = profiles?.find(p => p.name === profileName)
    if (!prof) {
      setForm(f => ({ ...f, profile: profileName }))
      return
    }
    // Profile CIDR math result pre-fills client AllowedIPs (network access).
    const resolvedIPs = (prof.resolved_allowed_ips || []).join(', ')
    setForm(f => ({
      ...f,
      profile: profileName,
      pka: prof.persistent_keepalive != null ? String(prof.persistent_keepalive) : f.pka,
      clientDNS: prof.client_dns || f.clientDNS,
      clientMTU: prof.client_mtu != null ? String(prof.client_mtu) : f.clientMTU,
      clientAllowedIPs: resolvedIPs || f.clientAllowedIPs,
      generatePSK: prof.use_preshared_key || f.generatePSK,
    }))
  }

  // Re-sync form when peer data changes (edit mode)
  const [lastPeerId, setLastPeerId] = useState(peer?.id)
  if (peer && peer.id !== lastPeerId) {
    setLastPeerId(peer.id)
    setForm(peerToFormData(peer))
  }

  function handleSubmit(e) {
    e.preventDefault()
    setCidrError('')
    setClientAddressError('')
    setEndpointError('')

    if (form.endpoint) {
      if (!form.endpoint.includes(':')) {
        setEndpointError('Must be host:port format')
        return
      }
    }
    if (form.clientAddress) {
      if (!/^(\d{1,3}\.){3}\d{1,3}\/\d{1,2}$/.test(form.clientAddress)) {
        setClientAddressError('Must be CIDR format (e.g. 10.0.10.3/24)')
        return
      }
    }

    if (form.clientAllowedIPs) {
      const cidrs = form.clientAllowedIPs.split(',').map(s => s.trim()).filter(Boolean)
      const invalid = cidrs.filter(c => !isValidCIDR(c))
      if (invalid.length > 0) {
        setCidrError(`Invalid CIDR(s): ${invalid.join(', ')}`)
        return
      }
    }

    const body = { friendly_name: form.name, notes: form.notes || undefined }

    // Profile or custom — server AllowedIPs is auto-derived from client_address.
    if (form.profile && form.profile !== '__custom__') {
      body.profile = form.profile
    }

    if (form.clientAddress) body.client_address = form.clientAddress
    if (form.endpoint) body.configured_endpoint = form.endpoint
    if (form.pka !== '') body.persistent_keepalive = parseInt(form.pka, 10)
    if (form.clientDNS) body.client_dns = form.clientDNS
    if (form.clientMTU !== '') body.client_mtu = parseInt(form.clientMTU, 10)
    if (form.clientAllowedIPs) body.client_allowed_ips = form.clientAllowedIPs
    if (mode === 'create' && form.generatePSK) body.preshared_key = 'auto'

    // For edit mode, handle nullable fields
    if (mode === 'edit' && peer) {
      if (form.endpoint !== (peer.configured_endpoint || '')) body.configured_endpoint = form.endpoint
      if (form.clientAddress !== (peer.client_address || '')) body.client_address = form.clientAddress
      if (form.pka === '' && peer.persistent_keepalive != null) body.persistent_keepalive = null
      if (form.clientMTU === '' && peer.client_mtu != null) body.client_mtu = null
      if (form.clientAllowedIPs !== (peer.client_allowed_ips || '')) body.client_allowed_ips = form.clientAllowedIPs
      if (form.clientDNS !== (peer.client_dns || '')) body.client_dns = form.clientDNS

      // Handle profile change
      if (form.profile && form.profile !== '__custom__' && form.profile !== (peer.profile || '')) {
        body.profile = form.profile
      } else if ((!form.profile || form.profile === '__custom__') && peer.profile) {
        body.profile = null
      }
    }

    onSubmit(body)
  }

  return (
    <form onSubmit={handleSubmit} className="space-y-6 max-w-lg">
      <TooltipProvider>
      {/* ── General ── */}
      <fieldset className="space-y-3">
        <legend className="text-xs font-medium uppercase tracking-wider text-muted-foreground">General</legend>
        <div>
          <FieldLabel>Friendly Name *</FieldLabel>
          <Input value={form.name} onChange={e => setForm({ ...form, name: e.target.value })}
            placeholder="e.g. alice-laptop" required /></div>
        {hasProfiles && (<div>
          <FieldLabel hint="Reusable template that defines network access, DNS, MTU, and other defaults. Choose 'Custom' to set all fields manually.">
            Profile
          </FieldLabel>
          <Select value={form.profile} onValueChange={applyProfile}>
            <SelectTrigger><SelectValue placeholder="Select a profile..." /></SelectTrigger>
            <SelectContent>
              {(profiles || []).map(p => (<SelectItem key={p.name} value={p.name}>{p.name}</SelectItem>))}
              <SelectItem value="__custom__">Custom (manual)</SelectItem>
            </SelectContent>
          </Select></div>)}
        {selectedProfile && (
          <Card><CardHeader className="pb-2"><CardTitle className="text-sm">Network Access Preview</CardTitle></CardHeader>
            <CardContent className="text-xs space-y-1">
              <p>{selectedProfile.resolved_allowed_ips?.length || 0} routes → client AllowedIPs</p>
              <div className="flex flex-wrap gap-1">
                {(selectedProfile.resolved_allowed_ips || []).slice(0, 10).map(ip => (
                  <Badge key={ip} variant="outline" className="text-xs font-mono">{ip}</Badge>))}
                {(selectedProfile.resolved_allowed_ips?.length || 0) > 10 && (
                  <Badge variant="secondary">+{selectedProfile.resolved_allowed_ips.length - 10} more</Badge>)}
              </div>
              {selectedProfile.route_warning && <p className="text-amber-600 dark:text-amber-400">{selectedProfile.route_warning}</p>}
            </CardContent></Card>)}
        <div>
          <FieldLabel>Notes</FieldLabel>
          <Input value={form.notes} onChange={e => setForm({ ...form, notes: e.target.value })}
            placeholder="Optional notes" /></div>
      </fieldset>

      {/* ── Tunnel Identity ── */}
      <fieldset className="space-y-3">
        <legend className="text-xs font-medium uppercase tracking-wider text-muted-foreground">Tunnel Identity</legend>
        <div>
          <FieldLabel hint="Unique IP for this peer inside the VPN. Written into the client .conf as [Interface] Address. Also used as the server-side [Peer] AllowedIPs (/32). Format: 10.0.10.3/24.">
            Tunnel Address *
          </FieldLabel>
          <Input value={form.clientAddress} onChange={e => setForm({ ...form, clientAddress: e.target.value })}
            onBlur={() => { if (form.clientAddress && !/^(\d{1,3}\.){3}\d{1,3}\/\d{1,2}$/.test(form.clientAddress)) setClientAddressError('Must be CIDR format (e.g. 10.0.10.3/24)'); else setClientAddressError('') }}
            placeholder="e.g. 10.0.10.3/24"
            className={clientAddressError ? 'border-red-500' : ''} />
          {clientAddressError && <p className="text-xs text-red-500 mt-1">{clientAddressError}</p>}
        </div>
      </fieldset>

      {/* ── Server-side (wg.conf [Peer]) ── */}
      <fieldset className="space-y-3">
        <legend className="text-xs font-medium uppercase tracking-wider text-muted-foreground">Server-side (wg.conf [Peer])</legend>
        <div>
          <FieldLabel hint="Where the server sends packets to this peer. Only for site-to-site peers with a static IP. Leave empty for mobile/laptop clients. Format: host:port.">
            Endpoint
          </FieldLabel>
          <Input value={form.endpoint} onChange={e => setForm({ ...form, endpoint: e.target.value })}
            onBlur={() => { if (form.endpoint && !form.endpoint.includes(':')) setEndpointError('Must be host:port format'); else setEndpointError('') }}
            placeholder="host:port"
            className={endpointError ? 'border-red-500' : ''} />
          {endpointError && <p className="text-xs text-red-500 mt-1">{endpointError}</p>}
        </div>

        <div>
          <FieldLabel hint="Keepalive interval in seconds to maintain the tunnel through NAT. Typical: 25. Empty = omit.">
            PersistentKeepalive
          </FieldLabel>
          <Input type="number" min="0" max="65535" value={form.pka} onChange={e => setForm({ ...form, pka: e.target.value })}
            placeholder="seconds" /></div>

        {mode === 'create' && (
          <div className="flex items-center gap-2">
            <input id="generatePSK" type="checkbox"
              checked={form.generatePSK}
              onChange={(e) => setForm({ ...form, generatePSK: e.target.checked })}
              className="h-4 w-4 rounded border-input" />
            <FieldLabel hint="Generate a random pre-shared key for post-quantum resistance. Written into both server and client configs.">
              Generate PresharedKey
            </FieldLabel>
          </div>
        )}
        {mode === 'edit' && (
          <div>
            <FieldLabel hint="PresharedKey status for this peer. The actual key value is never shown. Use the Rotate Keys action to generate a new keypair and PSK.">
              PresharedKey
            </FieldLabel>
            <div className="mt-1">
              {peer?.has_preshared_key
                ? <span className="text-sm text-green-600 dark:text-green-400 font-medium">Set</span>
                : <span className="text-sm text-muted-foreground">Not set</span>}
            </div>
          </div>
        )}

        {mode === 'edit' && peer?.last_seen_endpoint && (
          <div>
            <FieldLabel hint="Last known IP:port from the WireGuard kernel. Updates automatically.">
              Last Seen Endpoint
            </FieldLabel>
            <div className="flex items-center gap-2">
              <Input value={peer.last_seen_endpoint} readOnly className="bg-muted" />
              <Button type="button" variant="outline" size="sm"
                onClick={() => { navigator.clipboard.writeText(peer.last_seen_endpoint); toast.success('Copied') }}>
                Copy
              </Button>
            </div>
          </div>
        )}
      </fieldset>

      {/* ── Client download config (.conf) ── */}
      <fieldset className="space-y-3">
        <legend className="text-xs font-medium uppercase tracking-wider text-muted-foreground">Client download config (.conf)</legend>
        <div>
          <FieldLabel hint="Which traffic the client routes through VPN. Written into client .conf as [Peer] AllowedIPs. Example: '10.0.0.0/8' for split-tunnel, '0.0.0.0/0, ::/0' for full-tunnel.">
            Allowed IPs *
          </FieldLabel>
          <Input value={form.clientAllowedIPs} onChange={e => setForm({ ...form, clientAllowedIPs: e.target.value })}
            placeholder="0.0.0.0/0, ::/0" /></div>
        <div>
          <FieldLabel hint="DNS servers while connected. Written into client .conf as [Interface] DNS. Empty = omit.">
            DNS
          </FieldLabel>
          <Input value={form.clientDNS} onChange={e => setForm({ ...form, clientDNS: e.target.value })}
            placeholder="1.1.1.1, 8.8.8.8" /></div>
        <div>
          <FieldLabel hint="Tunnel MTU. Written into client .conf as [Interface] MTU. Empty = omit (auto-detect).">
            MTU
          </FieldLabel>
          <Input type="number" min="0" max="9000" value={form.clientMTU} onChange={e => setForm({ ...form, clientMTU: e.target.value })}
            placeholder="bytes" /></div>
      </fieldset>
      </TooltipProvider>

      {error && <Alert variant="destructive"><AlertDescription>{error.message}</AlertDescription></Alert>}
      {cidrError && <Alert variant="destructive"><AlertDescription>{cidrError}</AlertDescription></Alert>}
      <div className="flex gap-2">
        <Button type="submit" disabled={isPending || !form.name || !isConnected || (mode === 'create' && (!form.clientAllowedIPs || !form.clientAddress))}>
          {isPending ? 'Saving...' : mode === 'create' ? 'Create Peer' : 'Save Changes'}
        </Button>
        <Button type="button" variant="outline" onClick={onCancel}>Cancel</Button>
      </div>
    </form>
  )
}
