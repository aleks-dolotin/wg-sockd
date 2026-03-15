import { useState } from 'react'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { useProfiles } from '@/api/hooks'
import { createProfile, updateProfile, deleteProfile } from '@/api/client'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Badge } from '@/components/ui/badge'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { Dialog, DialogContent, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog'

export default function ProfilesPage() {
  const { data: profiles, isLoading, error } = useProfiles()
  const queryClient = useQueryClient()
  const [editing, setEditing] = useState(null) // null | 'new' | profile object
  const [formData, setFormData] = useState({ name: '', display_name: '', allowed_ips: '', exclude_ips: '', description: '' })
  const [deleteError, setDeleteError] = useState(null)

  const saveMut = useMutation({
    mutationFn: (data) => {
      const payload = {
        ...data,
        allowed_ips: data.allowed_ips.split(',').map(s => s.trim()).filter(Boolean),
        exclude_ips: data.exclude_ips ? data.exclude_ips.split(',').map(s => s.trim()).filter(Boolean) : [],
      }
      return editing === 'new' ? createProfile(payload) : updateProfile(editing.name, payload)
    },
    onSuccess: () => { queryClient.invalidateQueries({ queryKey: ['profiles'] }); setEditing(null) },
  })

  const deleteMut = useMutation({
    mutationFn: (name) => deleteProfile(name),
    onSuccess: () => { queryClient.invalidateQueries({ queryKey: ['profiles'] }); setDeleteError(null) },
    onError: (err) => setDeleteError(err.message),
  })

  function openNew() {
    setFormData({ name: '', display_name: '', allowed_ips: '', exclude_ips: '', description: '' })
    setEditing('new')
  }

  function openEdit(p) {
    setFormData({
      name: p.name,
      display_name: p.display_name || '',
      allowed_ips: (p.allowed_ips || []).join(', '),
      exclude_ips: (p.exclude_ips || []).join(', '),
      description: p.description || '',
    })
    setEditing(p)
  }

  if (isLoading) return <p className="text-gray-500">Loading profiles...</p>
  if (error) return <p className="text-red-500">Error: {error.message}</p>

  return (
    <div className="space-y-4">
      <div className="flex items-center justify-between">
        <h2 className="text-2xl font-semibold tracking-tight">Profiles</h2>
        <Button onClick={openNew}>Create Profile</Button>
      </div>

      {deleteError && <Alert variant="destructive"><AlertDescription>{deleteError}</AlertDescription></Alert>}

      <div className="grid gap-3">
        {(profiles || []).map(p => (
          <Card key={p.name}>
            <CardHeader className="pb-2">
              <div className="flex items-center justify-between">
                <CardTitle className="text-base">
                  {p.display_name || p.name}
                  {p.is_default && <Badge variant="secondary" className="ml-2">Default</Badge>}
                </CardTitle>
                <div className="space-x-1">
                  <Button variant="ghost" size="sm" onClick={() => openEdit(p)}>Edit</Button>
                  <Button variant="ghost" size="sm" className="text-red-600" onClick={() => deleteMut.mutate(p.name)}>Delete</Button>
                </div>
              </div>
            </CardHeader>
            <CardContent className="text-sm text-gray-600 space-y-1">
              {p.description && <p>{p.description}</p>}
              <p>{p.resolved_allowed_ips?.length || 0} resolved routes | {p.peer_count || 0} peers</p>
            </CardContent>
          </Card>
        ))}
      </div>

      {/* Create/Edit dialog */}
      <Dialog open={editing !== null} onOpenChange={() => setEditing(null)}>
        <DialogContent>
          <DialogHeader><DialogTitle>{editing === 'new' ? 'Create Profile' : 'Edit Profile'}</DialogTitle></DialogHeader>
          <form onSubmit={e => { e.preventDefault(); saveMut.mutate(formData) }} className="space-y-3">
            <div><label className="text-sm font-medium">Name</label>
              <Input value={formData.name} onChange={e => setFormData({ ...formData, name: e.target.value })}
                placeholder="e.g. full-tunnel" disabled={editing !== 'new'} required /></div>
            <div><label className="text-sm font-medium">Display Name</label>
              <Input value={formData.display_name} onChange={e => setFormData({ ...formData, display_name: e.target.value })}
                placeholder="Full Tunnel VPN" /></div>
            <div><label className="text-sm font-medium">Allowed IPs (comma-separated CIDRs) *</label>
              <Input value={formData.allowed_ips} onChange={e => setFormData({ ...formData, allowed_ips: e.target.value })}
                placeholder="0.0.0.0/0" required /></div>
            <div><label className="text-sm font-medium">Exclude IPs (optional)</label>
              <Input value={formData.exclude_ips} onChange={e => setFormData({ ...formData, exclude_ips: e.target.value })}
                placeholder="10.0.0.0/8, 172.16.0.0/12" /></div>
            <div><label className="text-sm font-medium">Description</label>
              <Input value={formData.description} onChange={e => setFormData({ ...formData, description: e.target.value })}
                placeholder="Routes all traffic through VPN" /></div>
            {saveMut.error && <Alert variant="destructive"><AlertDescription>{saveMut.error.message}</AlertDescription></Alert>}
            <DialogFooter>
              <Button type="button" variant="outline" onClick={() => setEditing(null)}>Cancel</Button>
              <Button type="submit" disabled={saveMut.isPending}>{saveMut.isPending ? 'Saving...' : 'Save'}</Button>
            </DialogFooter>
          </form>
        </DialogContent>
      </Dialog>
    </div>
  )
}
