import { useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { useProfiles } from '@/api/hooks'
import { createPeer } from '@/api/client'
import { isValidCIDR } from '@/lib/format'
import { useConnection } from '@/components/ConnectionContext'
import { usePageTitle } from '@/hooks/usePageTitle'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Badge } from '@/components/ui/badge'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { Dialog, DialogContent, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { toast } from 'sonner'

export default function PeerNewPage() {
  usePageTitle('Add Peer')
  const navigate = useNavigate()
  const queryClient = useQueryClient()
  const { data: profiles } = useProfiles()
  const { isConnected } = useConnection()
  const [name, setName] = useState('')
  const [profile, setProfile] = useState('')
  const [notes, setNotes] = useState('')
  const [customIPs, setCustomIPs] = useState('')
  const [cidrError, setCidrError] = useState('')
  const [result, setResult] = useState(null)
  const [endpoint, setEndpoint] = useState('')
  const [endpointError, setEndpointError] = useState('')
  const [pka, setPka] = useState('')
  const [clientDNS, setClientDNS] = useState('')
  const [clientMTU, setClientMTU] = useState('')
  const [showAdvanced, setShowAdvanced] = useState(false)

  const createMut = useMutation({
    mutationFn: (data) => createPeer(data),
    onSuccess: (data) => { queryClient.invalidateQueries({ queryKey: ['peers'] }); setResult(data) },
    onError: (err) => toast.error(err.message),
  })

  const selectedProfile = profiles?.find(p => p.name === profile)
  const hasProfiles = profiles && profiles.length > 0
  const isCustom = profile === '__custom__' || !hasProfiles

  function handleSubmit(e) {
    e.preventDefault()
    setCidrError('')
    // Validate endpoint.
    if (endpoint) {
      const parts = endpoint.split(':')
      if (parts.length < 2 || !parts[parts.length - 1]) {
        setEndpointError('Must be host:port format')
        return
      }
      setEndpointError('')
    }
    const body = { friendly_name: name, notes: notes || undefined }
    if (isCustom) {
      const cidrs = customIPs.split(',').map(s => s.trim()).filter(Boolean)
      const invalid = cidrs.filter(c => !isValidCIDR(c))
      if (invalid.length > 0) {
        setCidrError(`Invalid CIDR(s): ${invalid.join(', ')}`)
        return
      }
      body.allowed_ips = cidrs
    } else if (profile) {
      body.profile = profile
    }
    if (endpoint) body.configured_endpoint = endpoint
    if (pka !== '') body.persistent_keepalive = parseInt(pka, 10)
    if (clientDNS) body.client_dns = clientDNS
    if (clientMTU !== '') body.client_mtu = parseInt(clientMTU, 10)
    createMut.mutate(body)
  }

  return (
    <div className="space-y-4 max-w-lg mx-auto">
      <h2 className="text-2xl font-semibold tracking-tight">Add Peer</h2>
      {!result ? (
        <form onSubmit={handleSubmit} className="space-y-4">
          <div><label className="text-sm font-medium">Friendly Name *</label>
            <Input value={name} onChange={e => setName(e.target.value)} placeholder="e.g. alice-laptop" required /></div>
          {hasProfiles && (<div><label className="text-sm font-medium">Profile</label>
            <Select value={profile} onValueChange={setProfile}>
              <SelectTrigger><SelectValue placeholder="Select a profile..." /></SelectTrigger>
              <SelectContent>
                {(profiles || []).map(p => (<SelectItem key={p.name} value={p.name}>{p.name}</SelectItem>))}
                <SelectItem value="__custom__">Custom (manual CIDRs)</SelectItem>
              </SelectContent>
            </Select></div>)}
          {selectedProfile && (
            <Card><CardHeader className="pb-2"><CardTitle className="text-sm">Network Access Preview</CardTitle></CardHeader>
              <CardContent className="text-xs space-y-1">
                <p>{selectedProfile.resolved_allowed_ips?.length || 0} routes</p>
                <div className="flex flex-wrap gap-1">
                  {(selectedProfile.resolved_allowed_ips || []).slice(0, 10).map(ip => (
                    <Badge key={ip} variant="outline" className="text-xs font-mono">{ip}</Badge>))}
                  {(selectedProfile.resolved_allowed_ips?.length || 0) > 10 && (
                    <Badge variant="secondary">+{selectedProfile.resolved_allowed_ips.length - 10} more</Badge>)}
                </div>
                {selectedProfile.route_warning && <p className="text-amber-600 dark:text-amber-400">{selectedProfile.route_warning}</p>}
              </CardContent></Card>)}
          {isCustom && (<div><label className="text-sm font-medium">Allowed IPs (comma-separated CIDRs)</label>
            <Input value={customIPs} onChange={e => setCustomIPs(e.target.value)} placeholder="10.0.0.0/24, 192.168.1.0/24" /></div>)}
          <div><label className="text-sm font-medium">Notes</label>
            <Input value={notes} onChange={e => setNotes(e.target.value)} placeholder="Optional notes" /></div>
          <div><label className="text-sm font-medium">Endpoint</label>
            <Input value={endpoint} onChange={e => setEndpoint(e.target.value)}
              onBlur={() => { if (endpoint && !endpoint.includes(':')) setEndpointError('Must be host:port format'); else setEndpointError('') }}
              placeholder="host:port (for site-to-site peers)"
              className={endpointError ? 'border-red-500' : ''} />
            {endpointError && <p className="text-xs text-red-500 mt-1">{endpointError}</p>}</div>
          <button type="button" onClick={() => setShowAdvanced(!showAdvanced)}
            className="text-sm text-muted-foreground hover:text-foreground flex items-center gap-1">
            <span className={`transition-transform ${showAdvanced ? 'rotate-90' : ''}`}>▶</span>
            Advanced Settings
          </button>
          {showAdvanced && (
            <div className="space-y-3 pl-2 border-l-2 border-muted">
              <div><label className="text-sm font-medium">PersistentKeepalive</label>
                <Input type="number" min="0" max="65535" value={pka} onChange={e => setPka(e.target.value)}
                  placeholder="0 = off, empty = inherit" /></div>
              <div><label className="text-sm font-medium">Client DNS</label>
                <Input value={clientDNS} onChange={e => setClientDNS(e.target.value)}
                  placeholder="1.1.1.1, 8.8.8.8" /></div>
              <div><label className="text-sm font-medium">Client MTU</label>
                <Input type="number" min="0" max="9000" value={clientMTU} onChange={e => setClientMTU(e.target.value)}
                  placeholder="auto (empty = inherit)" /></div>
            </div>
          )}
          {createMut.error && <Alert variant="destructive"><AlertDescription>{createMut.error.message}</AlertDescription></Alert>}
          {cidrError && <Alert variant="destructive"><AlertDescription>{cidrError}</AlertDescription></Alert>}
          <div className="flex gap-2">
            <Button type="submit" disabled={createMut.isPending || !name || !isConnected || (isCustom && !customIPs)}>{createMut.isPending ? 'Creating...' : 'Create Peer'}</Button>
            <Button type="button" variant="outline" onClick={() => navigate('/peers')}>Cancel</Button>
          </div>
        </form>
      ) : (
        <Dialog open={true} onOpenChange={() => navigate('/peers')}>
          <DialogContent>
            <DialogHeader><DialogTitle>Peer Created</DialogTitle></DialogHeader>
            <Alert><AlertDescription>Save this configuration now. The private key will not be shown again.</AlertDescription></Alert>
            <div className="flex justify-center p-4">
              <img src={`data:image/png;base64,${result.qr}`} alt="QR Code" className="w-64 h-64" />
            </div>
            <DialogFooter>
              <Button variant="outline" onClick={() => {
                const blob = new Blob([result.config], { type: 'text/plain' })
                const url = URL.createObjectURL(blob)
                const a = document.createElement('a')
                a.href = url
                a.download = `${result.friendly_name || 'peer'}.conf`
                a.click()
                URL.revokeObjectURL(url)
              }}>Download .conf</Button>
              <Button onClick={() => { toast.success(`Peer ${name} created`); navigate('/peers') }}>Done</Button>
            </DialogFooter>
          </DialogContent>
        </Dialog>
      )}
    </div>
  )
}
