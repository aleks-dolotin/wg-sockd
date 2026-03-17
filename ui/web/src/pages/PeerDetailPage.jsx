import { useState, useEffect } from 'react'
import { useParams, useNavigate } from 'react-router-dom'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { usePeer, useProfiles } from '@/api/hooks'
import { updatePeer, approvePeer } from '@/api/client'
import { useConnection } from '@/components/ConnectionContext'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Badge } from '@/components/ui/badge'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'

export default function PeerDetailPage() {
  const { id } = useParams()
  const navigate = useNavigate()
  const queryClient = useQueryClient()
  const { data: peer, isLoading, error } = usePeer(id)
  const { data: profiles } = useProfiles()
  const { isConnected } = useConnection()

  const [name, setName] = useState('')
  const [notes, setNotes] = useState('')
  const [allowedIPs, setAllowedIPs] = useState('')
  const [profile, setProfile] = useState('')
  const [saving, setSaving] = useState(false)
  const [saveError, setSaveError] = useState(null)
  const [saveSuccess, setSaveSuccess] = useState(false)

  useEffect(() => {
    if (peer) {
      setName(peer.friendly_name || '')
      setNotes(peer.notes || '')
      setAllowedIPs(peer.allowed_ips?.join(', ') || '')
      setProfile(peer.profile || '')
    }
  }, [peer])

  const approveMut = useMutation({
    mutationFn: (peerId) => approvePeer(peerId),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['peers'] })
      queryClient.invalidateQueries({ queryKey: ['peers', id] })
    },
  })

  const handleSave = async () => {
    setSaving(true)
    setSaveError(null)
    setSaveSuccess(false)

    const update = {}
    if (name !== (peer.friendly_name || '')) update.friendly_name = name
    if (notes !== (peer.notes || '')) update.notes = notes

    if (profile && profile !== (peer.profile || '')) {
      update.profile = profile
    } else if (!profile && peer.profile) {
      update.profile = null
      const ips = allowedIPs.split(',').map(s => s.trim()).filter(Boolean)
      if (ips.length > 0) update.allowed_ips = ips
    }

    if (!profile) {
      const newIPs = allowedIPs.split(',').map(s => s.trim()).filter(Boolean)
      const oldIPs = peer.allowed_ips || []
      if (JSON.stringify(newIPs) !== JSON.stringify(oldIPs)) {
        update.allowed_ips = newIPs
      }
    }

    if (Object.keys(update).length === 0) {
      setSaving(false)
      setSaveSuccess(true)
      return
    }

    try {
      await updatePeer(id, update)
      queryClient.invalidateQueries({ queryKey: ['peers'] })
      queryClient.invalidateQueries({ queryKey: ['peers', id] })
      setSaveSuccess(true)
    } catch (err) {
      setSaveError(err.message)
    } finally {
      setSaving(false)
    }
  }

  if (isLoading) return <p className="text-gray-500">Loading peer…</p>
  if (error) return <p className="text-red-500">Error: {error.message}</p>
  if (!peer) return <p className="text-gray-500">Peer not found</p>

  return (
    <div className="max-w-2xl space-y-6">
      <div className="flex items-center justify-between">
        <h2 className="text-2xl font-semibold tracking-tight">Edit Peer</h2>
        <Button variant="outline" onClick={() => navigate('/')}>Back</Button>
      </div>

      {peer.auto_discovered && !peer.enabled && (
        <Card className="border-orange-200 bg-orange-50">
          <CardContent className="pt-4">
            <div className="flex items-center justify-between">
              <div>
                <p className="font-medium text-orange-800">Auto-discovered peer (disabled)</p>
                <p className="text-sm text-orange-600">This peer was found in WireGuard but not in the database. Approve to enable it.</p>
              </div>
              <Button
                disabled={!isConnected || approveMut.isPending}
                onClick={() => approveMut.mutate(peer.id)}
              >
                {approveMut.isPending ? 'Approving…' : 'Approve'}
              </Button>
            </div>
          </CardContent>
        </Card>
      )}

      <Card>
        <CardHeader>
          <CardTitle>Peer Details</CardTitle>
        </CardHeader>
        <CardContent className="space-y-4">
          <div>
            <label className="text-sm font-medium text-gray-700">Public Key</label>
            <p className="font-mono text-sm text-gray-500 mt-1">{peer.public_key}</p>
          </div>

          <div>
            <label className="text-sm font-medium text-gray-700">Status</label>
            <div className="mt-1">
              <Badge variant={peer.enabled ? 'default' : 'secondary'}>
                {peer.enabled ? 'Enabled' : 'Disabled'}
              </Badge>
              {peer.auto_discovered && (
                <Badge variant="secondary" className="ml-2 text-orange-600">Auto-discovered</Badge>
              )}
            </div>
          </div>

          <div>
            <label className="text-sm font-medium text-gray-700" htmlFor="name">Name</label>
            <Input
              id="name"
              value={name}
              onChange={(e) => setName(e.target.value)}
              className="mt-1"
            />
          </div>

          <div>
            <label className="text-sm font-medium text-gray-700" htmlFor="profile">Profile</label>
            <select
              id="profile"
              value={profile}
              onChange={(e) => setProfile(e.target.value)}
              className="mt-1 w-full rounded-md border border-gray-300 px-3 py-2 text-sm"
            >
              <option value="">No profile (custom IPs)</option>
              {(profiles || []).map(p => (
                <option key={p.name} value={p.name}>{p.name}</option>
              ))}
            </select>
          </div>

          {!profile && (
            <div>
              <label className="text-sm font-medium text-gray-700" htmlFor="allowedIPs">Allowed IPs</label>
              <Input
                id="allowedIPs"
                value={allowedIPs}
                onChange={(e) => setAllowedIPs(e.target.value)}
                placeholder="e.g. 10.0.0.2/32, 192.168.1.0/24"
                className="mt-1"
              />
              <p className="text-xs text-gray-400 mt-1">Comma-separated CIDRs</p>
            </div>
          )}

          <div>
            <label className="text-sm font-medium text-gray-700" htmlFor="notes">Notes</label>
            <Input
              id="notes"
              value={notes}
              onChange={(e) => setNotes(e.target.value)}
              className="mt-1"
            />
          </div>

          {saveError && (
            <p className="text-sm text-red-500">{saveError}</p>
          )}
          {saveSuccess && (
            <p className="text-sm text-green-600">Saved successfully</p>
          )}

          <div className="flex gap-2 pt-2">
            <Button
              onClick={handleSave}
              disabled={saving || !isConnected}
            >
              {saving ? 'Saving…' : 'Save Changes'}
            </Button>
            <Button variant="outline" onClick={() => navigate('/')}>Cancel</Button>
          </div>
        </CardContent>
      </Card>
    </div>
  )
}

