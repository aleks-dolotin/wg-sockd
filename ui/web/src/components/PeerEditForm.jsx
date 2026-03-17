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

  // Re-sync form when peer data changes (e.g., after mutation invalidation)
  const peerId = peer?.id
  const [lastPeerId, setLastPeerId] = useState(peerId)
  if (peerId !== lastPeerId) {
    setLastPeerId(peerId)
    setName(peer?.friendly_name || '')
    setNotes(peer?.notes || '')
    setAllowedIPs(peer?.allowed_ips?.join(', ') || '')
    setProfile(peer?.profile || '')
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

  const handleSave = () => {
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

        <div className="flex gap-2 pt-2">
          <Button onClick={handleSave} disabled={saveMut.isPending || !isConnected}>
            {saveMut.isPending ? 'Saving…' : 'Save Changes'}
          </Button>
        </div>
      </CardContent>
    </Card>
  )
}
