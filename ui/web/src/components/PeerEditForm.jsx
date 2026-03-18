import { useState } from 'react'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { useProfiles } from '@/api/hooks'
import { updatePeer } from '@/api/client'
import { useConnection } from '@/components/ConnectionContext'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { toast } from 'sonner'

export default function PeerEditForm({ peer }) {
  const queryClient = useQueryClient()
  const { data: profiles } = useProfiles()
  const { isConnected } = useConnection()

  const [name, setName] = useState(peer?.friendly_name || '')
  const [notes, setNotes] = useState(peer?.notes || '')
  const [allowedIPs, setAllowedIPs] = useState(peer?.allowed_ips?.join(', ') || '')
  const [profile, setProfile] = useState(peer?.profile || '')
  const [endpoint, setEndpoint] = useState(peer?.configured_endpoint || '')
  const [endpointError, setEndpointError] = useState('')
  const [pka, setPka] = useState(peer?.persistent_keepalive != null ? String(peer.persistent_keepalive) : '')
  const [clientDNS, setClientDNS] = useState(peer?.client_dns || '')
  const [clientMTU, setClientMTU] = useState(peer?.client_mtu != null ? String(peer.client_mtu) : '')
  const [showAdvanced, setShowAdvanced] = useState(false)

  // Re-sync form when peer data changes (e.g., after mutation invalidation)
  const peerId = peer?.id
  const [lastPeerId, setLastPeerId] = useState(peerId)
  if (peerId !== lastPeerId) {
    setLastPeerId(peerId)
    setName(peer?.friendly_name || '')
    setNotes(peer?.notes || '')
    setAllowedIPs(peer?.allowed_ips?.join(', ') || '')
    setProfile(peer?.profile || '')
    setEndpoint(peer?.configured_endpoint || '')
    setEndpointError('')
    setPka(peer?.persistent_keepalive != null ? String(peer.persistent_keepalive) : '')
    setClientDNS(peer?.client_dns || '')
    setClientMTU(peer?.client_mtu != null ? String(peer.client_mtu) : '')
  }

  const saveMut = useMutation({
    mutationFn: (update) => updatePeer(peer.id, update),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['peers'] })
      queryClient.invalidateQueries({ queryKey: ['peers', String(peer.id)] })
      toast.success('Changes saved')
    },
    onError: (err) => toast.error(err.message),
  })

  const validateEndpoint = (value) => {
    if (!value) { setEndpointError(''); return true }
    const parts = value.split(':')
    if (parts.length < 2 || !parts[parts.length - 1]) {
      setEndpointError('Must be host:port format')
      return false
    }
    setEndpointError('')
    return true
  }

  const handleSave = () => {
    if (!validateEndpoint(endpoint)) return

    const update = {}
    if (name !== (peer.friendly_name || '')) update.friendly_name = name
    if (notes !== (peer.notes || '')) update.notes = notes

    if (profile && profile !== (peer.profile || '')) {
      update.profile = profile
    } else if (!profile && peer.profile) {
      update.profile = null
    }

    if (!profile) {
      const newIPs = allowedIPs.split(',').map(s => s.trim()).filter(Boolean)
      const oldIPs = peer.allowed_ips || []
      if (JSON.stringify(newIPs) !== JSON.stringify(oldIPs)) {
        update.allowed_ips = newIPs
      }
    }

    if (endpoint !== (peer.configured_endpoint || '')) update.configured_endpoint = endpoint
    if (pka !== '' && pka !== (peer.persistent_keepalive != null ? String(peer.persistent_keepalive) : '')) {
      update.persistent_keepalive = parseInt(pka, 10)
    } else if (pka === '' && peer.persistent_keepalive != null) {
      update.persistent_keepalive = null
    }
    if (clientDNS !== (peer.client_dns || '')) update.client_dns = clientDNS
    if (clientMTU !== '' && clientMTU !== (peer.client_mtu != null ? String(peer.client_mtu) : '')) {
      update.client_mtu = parseInt(clientMTU, 10)
    } else if (clientMTU === '' && peer.client_mtu != null) {
      update.client_mtu = null
    }

    if (Object.keys(update).length === 0) {
      toast.success('No changes to save')
      return
    }

    saveMut.mutate(update)
  }

  return (
    <Card>
      <CardHeader>
        <CardTitle>Edit</CardTitle>
      </CardHeader>
      <CardContent className="space-y-4">
        <div>
          <label className="text-sm font-medium text-muted-foreground" htmlFor="name">Name</label>
          <Input id="name" value={name} onChange={(e) => setName(e.target.value)} className="mt-1" />
        </div>

        <div>
          <label className="text-sm font-medium text-muted-foreground" htmlFor="profile">Profile</label>
          <select
            id="profile"
            value={profile}
            onChange={(e) => setProfile(e.target.value)}
            className="mt-1 w-full rounded-md border border-input bg-background px-3 py-2 text-sm"
          >
            <option value="">No profile (custom IPs)</option>
            {(profiles || []).map(p => (
              <option key={p.name} value={p.name}>{p.name}</option>
            ))}
          </select>
        </div>

        {!profile && (
          <div>
            <label className="text-sm font-medium text-muted-foreground" htmlFor="allowedIPs">Allowed IPs</label>
            <Input
              id="allowedIPs"
              value={allowedIPs}
              onChange={(e) => setAllowedIPs(e.target.value)}
              placeholder="e.g. 10.0.0.2/32, 192.168.1.0/24"
              className="mt-1"
            />
            <p className="text-xs text-muted-foreground mt-1">Comma-separated CIDRs</p>
          </div>
        )}

        <div>
          <label className="text-sm font-medium text-muted-foreground" htmlFor="notes">Notes</label>
          <Input id="notes" value={notes} onChange={(e) => setNotes(e.target.value)} className="mt-1" />
        </div>

        <div>
          <label className="text-sm font-medium text-muted-foreground" htmlFor="endpoint">Endpoint</label>
          <Input
            id="endpoint"
            value={endpoint}
            onChange={(e) => setEndpoint(e.target.value)}
            onBlur={() => validateEndpoint(endpoint)}
            placeholder="host:port (for site-to-site)"
            className={`mt-1 ${endpointError ? 'border-red-500' : ''}`}
          />
          {endpointError && <p className="text-xs text-red-500 mt-1">{endpointError}</p>}
          {!endpointError && peer?.resolved_client_persistent_keepalive_source && (
            <p className="text-xs text-muted-foreground mt-1">Server-side: written to wg0.conf [Peer] section</p>
          )}
        </div>

        <div>
          <button
            type="button"
            onClick={() => setShowAdvanced(!showAdvanced)}
            className="text-sm text-muted-foreground hover:text-foreground flex items-center gap-1"
          >
            <span className={`transition-transform ${showAdvanced ? 'rotate-90' : ''}`}>▶</span>
            Advanced Settings
          </button>
        </div>

        {showAdvanced && (
          <div className="space-y-4 pl-2 border-l-2 border-muted">
            <div>
              <label className="text-sm font-medium text-muted-foreground" htmlFor="pka">PersistentKeepalive</label>
              <Input
                id="pka"
                type="number"
                min="0"
                max="65535"
                value={pka}
                onChange={(e) => setPka(e.target.value)}
                placeholder={peer?.resolved_client_persistent_keepalive != null ? `inherited: ${peer.resolved_client_persistent_keepalive} (${peer.resolved_client_persistent_keepalive_source})` : '0 = off'}
                className="mt-1"
              />
              <p className="text-xs text-muted-foreground mt-1">Server-side keepalive interval in seconds (0 = off, empty = inherit)</p>
            </div>

            <div>
              <label className="text-sm font-medium text-muted-foreground" htmlFor="clientDNS">Client DNS</label>
              <Input
                id="clientDNS"
                value={clientDNS}
                onChange={(e) => setClientDNS(e.target.value)}
                placeholder={peer?.resolved_client_dns ? `inherited: ${peer.resolved_client_dns} (${peer.resolved_client_dns_source})` : '1.1.1.1, 8.8.8.8'}
                className="mt-1"
              />
              <p className="text-xs text-muted-foreground mt-1">DNS servers for client download config (empty = inherit)</p>
            </div>

            <div>
              <label className="text-sm font-medium text-muted-foreground" htmlFor="clientMTU">Client MTU</label>
              <Input
                id="clientMTU"
                type="number"
                min="0"
                max="9000"
                value={clientMTU}
                onChange={(e) => setClientMTU(e.target.value)}
                placeholder={peer?.resolved_client_mtu ? `inherited: ${peer.resolved_client_mtu} (${peer.resolved_client_mtu_source})` : 'auto'}
                className="mt-1"
              />
              <p className="text-xs text-muted-foreground mt-1">MTU for client download config (empty = inherit, 0 = auto)</p>
            </div>
          </div>
        )}

        <div className="flex gap-2 pt-2">
          <Button onClick={handleSave} disabled={saveMut.isPending || !isConnected}>
            {saveMut.isPending ? 'Saving…' : 'Save Changes'}
          </Button>
        </div>
      </CardContent>
    </Card>
  )
}
