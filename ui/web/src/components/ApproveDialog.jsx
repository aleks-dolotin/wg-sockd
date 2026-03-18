import { useState } from 'react'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { useProfiles } from '@/api/hooks'
import { approvePeer } from '@/api/client'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Dialog, DialogContent, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { toast } from 'sonner'

export default function ApproveDialog({ peer, open, onClose }) {
  const queryClient = useQueryClient()
  const { data: profiles } = useProfiles()

  const [name, setName] = useState(peer?.friendly_name || '')
  const [profile, setProfile] = useState('')
  const [clientAddress, setClientAddress] = useState('')
  const [clientAddressError, setClientAddressError] = useState('')
  const [endpoint, setEndpoint] = useState('')
  const [clientDNS, setClientDNS] = useState('')
  const [clientMTU, setClientMTU] = useState('')
  const [pka, setPka] = useState('')

  const approveMut = useMutation({
    mutationFn: (body) => approvePeer(peer.id, body),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['peers'] })
      toast.success('Peer approved')
      onClose()
    },
    onError: (err) => toast.error(err.message),
  })

  const handleApprove = () => {
    if (!clientAddress) {
      setClientAddressError('Client Address is required')
      return
    }
    const cidrRegex = /^(\d{1,3}\.){3}\d{1,3}\/\d{1,2}$/
    if (!cidrRegex.test(clientAddress)) {
      setClientAddressError('Must be CIDR format (e.g. 10.0.0.2/32)')
      return
    }
    setClientAddressError('')

    const body = { client_address: clientAddress }
    if (name && name !== peer.friendly_name) body.friendly_name = name
    if (profile) body.profile = profile
    if (endpoint) body.configured_endpoint = endpoint
    if (clientDNS) body.client_dns = clientDNS
    if (clientMTU !== '') body.client_mtu = parseInt(clientMTU, 10)
    if (pka !== '') body.persistent_keepalive = parseInt(pka, 10)

    approveMut.mutate(body)
  }

  if (!peer) return null

  return (
    <Dialog open={open} onOpenChange={onClose}>
      <DialogContent className="max-w-md">
        <DialogHeader>
          <DialogTitle>Approve Peer</DialogTitle>
        </DialogHeader>
        <div className="space-y-4">
          <div>
            <label className="text-sm font-medium text-muted-foreground">Public Key</label>
            <Input value={peer.public_key} readOnly className="mt-1 bg-muted font-mono text-xs" />
          </div>

          {peer.last_seen_endpoint && (
            <div>
              <label className="text-sm font-medium text-muted-foreground">Last Seen Endpoint</label>
              <div className="mt-1 flex items-center gap-2">
                <Input value={peer.last_seen_endpoint} readOnly className="bg-muted font-mono text-xs" />
                <Button variant="outline" size="sm"
                  onClick={() => { navigator.clipboard.writeText(peer.last_seen_endpoint); toast.success('Copied') }}>
                  Copy
                </Button>
              </div>
            </div>
          )}

          <div>
            <label className="text-sm font-medium">Name *</label>
            <Input value={name} onChange={e => setName(e.target.value)} className="mt-1" />
          </div>

          <div>
            <label className="text-sm font-medium">Profile</label>
            <Select value={profile} onValueChange={setProfile}>
              <SelectTrigger className="mt-1"><SelectValue placeholder="Select a profile..." /></SelectTrigger>
              <SelectContent>
                <SelectItem value="__none__">No profile</SelectItem>
                {(profiles || []).map(p => (
                  <SelectItem key={p.name} value={p.name}>{p.name}</SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>

          <div>
            <label className="text-sm font-medium">Client Address * <span className="text-xs text-muted-foreground">(CIDR)</span></label>
            <Input value={clientAddress} onChange={e => setClientAddress(e.target.value)}
              placeholder="e.g. 10.0.0.2/32"
              className={`mt-1 ${clientAddressError ? 'border-red-500' : ''}`} />
            {clientAddressError && <p className="text-xs text-red-500 mt-1">{clientAddressError}</p>}
          </div>

          <div>
            <label className="text-sm font-medium">Configured Endpoint</label>
            <Input value={endpoint} onChange={e => setEndpoint(e.target.value)}
              placeholder={peer.last_seen_endpoint || 'host:port (leave empty for clients)'}
              className="mt-1" />
            <p className="text-xs text-muted-foreground mt-1">Leave empty for regular clients. Set for site-to-site peers.</p>
          </div>

          <div>
            <label className="text-sm font-medium">Client DNS</label>
            <Input value={clientDNS} onChange={e => setClientDNS(e.target.value)} placeholder="1.1.1.1" className="mt-1" />
          </div>

          <div className="grid grid-cols-2 gap-3">
            <div>
              <label className="text-sm font-medium">MTU</label>
              <Input type="number" value={clientMTU} onChange={e => setClientMTU(e.target.value)} placeholder="auto" className="mt-1" />
            </div>
            <div>
              <label className="text-sm font-medium">Keepalive</label>
              <Input type="number" value={pka} onChange={e => setPka(e.target.value)} placeholder="inherit" className="mt-1" />
            </div>
          </div>
        </div>

        <DialogFooter>
          <Button variant="outline" onClick={onClose}>Cancel</Button>
          <Button onClick={handleApprove} disabled={approveMut.isPending}>
            {approveMut.isPending ? 'Approving…' : 'Approve'}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
