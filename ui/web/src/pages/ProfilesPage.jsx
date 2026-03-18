import { useState } from 'react'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { useProfiles } from '@/api/hooks'
import { createProfile, updateProfile, deleteProfile } from '@/api/client'
import { usePageTitle } from '@/hooks/usePageTitle'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Badge } from '@/components/ui/badge'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { Skeleton } from '@/components/ui/skeleton'
import { toast } from 'sonner'

function ProfilesSkeleton() {
  return (
    <div className="space-y-4">
      <div className="flex items-center justify-between">
        <Skeleton className="h-8 w-32" />
        <Skeleton className="h-9 w-32" />
      </div>
      {[...Array(3)].map((_, i) => (
        <Card key={i}><CardContent className="pt-6 space-y-2">
          <Skeleton className="h-5 w-40" />
          <Skeleton className="h-4 w-64" />
        </CardContent></Card>
      ))}
    </div>
  )
}

export default function ProfilesPage() {
  usePageTitle('Profiles')
  const { data: profiles, isLoading, error } = useProfiles()
  const queryClient = useQueryClient()
  const [editing, setEditing] = useState(null)
  const [formData, setFormData] = useState({ name: '', allowed_ips: '', exclude_ips: '', description: '', endpoint: '', persistent_keepalive: '', client_dns: '', client_mtu: '' })
  const [deleteTarget, setDeleteTarget] = useState(null)
  const [deleteError, setDeleteError] = useState(null)

  const saveMut = useMutation({
    mutationFn: (data) => {
      const payload = {
        ...data,
        allowed_ips: data.allowed_ips.split(',').map(s => s.trim()).filter(Boolean),
        exclude_ips: data.exclude_ips ? data.exclude_ips.split(',').map(s => s.trim()).filter(Boolean) : [],
      }
      if (data.persistent_keepalive !== '') payload.persistent_keepalive = parseInt(data.persistent_keepalive, 10)
      else delete payload.persistent_keepalive
      if (data.client_mtu !== '') payload.client_mtu = parseInt(data.client_mtu, 10)
      else delete payload.client_mtu
      return editing === 'new' ? createProfile(payload) : updateProfile(editing.name, payload)
    },
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['profiles'] })
      toast.success(editing === 'new' ? 'Profile created' : 'Profile updated')
      setEditing(null)
    },
    onError: (err) => toast.error(err.message),
  })

  const deleteMut = useMutation({
    mutationFn: (name) => deleteProfile(name),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['profiles'] })
      toast.success('Profile deleted')
      setDeleteTarget(null)
      setDeleteError(null)
    },
    onError: (err) => setDeleteError(err.message),
  })

  function openNew() {
    setFormData({ name: '', allowed_ips: '', exclude_ips: '', description: '', endpoint: '', persistent_keepalive: '', client_dns: '', client_mtu: '' })
    setEditing('new')
  }

  function openEdit(p) {
    setFormData({
      name: p.name,
      allowed_ips: (p.allowed_ips || []).join(', '),
      exclude_ips: (p.exclude_ips || []).join(', '),
      description: p.description || '',
      endpoint: p.endpoint || '',
      persistent_keepalive: p.persistent_keepalive != null ? String(p.persistent_keepalive) : '',
      client_dns: p.client_dns || '',
      client_mtu: p.client_mtu != null ? String(p.client_mtu) : '',
    })
    setEditing(p)
  }

  if (isLoading) return <ProfilesSkeleton />
  if (error) return <p className="text-destructive">Error: {error.message}</p>

  return (
    <div className="space-y-4">
      <div className="flex items-center justify-between">
        <h2 className="text-2xl font-semibold tracking-tight">Profiles</h2>
        <Button onClick={openNew}>Create Profile</Button>
      </div>

      <div className="grid gap-3">
        {(profiles || []).map(p => (
          <Card key={p.name}>
            <CardHeader className="pb-2">
              <div className="flex items-center justify-between">
                <CardTitle className="text-base">
                  {p.name}
                  {p.is_default && <Badge variant="secondary" className="ml-2">Default</Badge>}
                </CardTitle>
                <div className="space-x-1">
                  <Button variant="ghost" size="sm" onClick={() => openEdit(p)}>Edit</Button>
                  <Button variant="ghost" size="sm" className="text-destructive" onClick={() => { setDeleteTarget(p); setDeleteError(null) }}>Delete</Button>
                </div>
              </div>
            </CardHeader>
            <CardContent className="text-sm text-muted-foreground space-y-1">
              {p.description && <p>{p.description}</p>}
              <p>{p.resolved_allowed_ips?.length || 0} resolved routes | {p.peer_count || 0} peers</p>
            </CardContent>
          </Card>
        ))}
      </div>

      {/* Delete confirmation dialog */}
      <Dialog open={!!deleteTarget} onOpenChange={() => { setDeleteTarget(null); setDeleteError(null) }}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Delete Profile</DialogTitle>
            <DialogDescription>
              Are you sure you want to delete profile &quot;{deleteTarget?.name}&quot;? This action cannot be undone.
            </DialogDescription>
          </DialogHeader>
          {deleteError && <Alert variant="destructive"><AlertDescription>{deleteError}</AlertDescription></Alert>}
          <DialogFooter>
            <Button variant="outline" onClick={() => { setDeleteTarget(null); setDeleteError(null) }}>Cancel</Button>
            <Button variant="destructive" onClick={() => deleteMut.mutate(deleteTarget?.name)} disabled={deleteMut.isPending}>
              {deleteMut.isPending ? 'Deleting...' : 'Delete'}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      {/* Create/Edit dialog */}
      <Dialog open={editing !== null} onOpenChange={() => setEditing(null)}>
        <DialogContent>
          <DialogHeader><DialogTitle>{editing === 'new' ? 'Create Profile' : 'Edit Profile'}</DialogTitle></DialogHeader>
          <form onSubmit={e => { e.preventDefault(); saveMut.mutate(formData) }} className="space-y-3">
            <div><label className="text-sm font-medium">Name</label>
              <Input value={formData.name} onChange={e => setFormData({ ...formData, name: e.target.value })}
                placeholder="e.g. full-tunnel" disabled={editing !== 'new'} required /></div>
            <div><label className="text-sm font-medium">Allowed IPs (comma-separated CIDRs) *</label>
              <Input value={formData.allowed_ips} onChange={e => setFormData({ ...formData, allowed_ips: e.target.value })}
                placeholder="0.0.0.0/0" required /></div>
            <div><label className="text-sm font-medium">Exclude IPs (optional)</label>
              <Input value={formData.exclude_ips} onChange={e => setFormData({ ...formData, exclude_ips: e.target.value })}
                placeholder="10.0.0.0/8, 172.16.0.0/12" /></div>
            <div><label className="text-sm font-medium">Description</label>
              <Input value={formData.description} onChange={e => setFormData({ ...formData, description: e.target.value })}
                placeholder="Routes all traffic through VPN" /></div>
            <div><label className="text-sm font-medium">Default Endpoint</label>
              <Input value={formData.endpoint} onChange={e => setFormData({ ...formData, endpoint: e.target.value })}
                placeholder="host:port (pre-filled for new peers)" /></div>
            <div><label className="text-sm font-medium">Default PersistentKeepalive</label>
              <Input type="number" min="0" max="65535" value={formData.persistent_keepalive} onChange={e => setFormData({ ...formData, persistent_keepalive: e.target.value })}
                placeholder="empty = inherit from global" /></div>
            <div><label className="text-sm font-medium">Default Client DNS</label>
              <Input value={formData.client_dns} onChange={e => setFormData({ ...formData, client_dns: e.target.value })}
                placeholder="1.1.1.1, 8.8.8.8 (for client download conf)" /></div>
            <div><label className="text-sm font-medium">Default Client MTU</label>
              <Input type="number" min="0" max="9000" value={formData.client_mtu} onChange={e => setFormData({ ...formData, client_mtu: e.target.value })}
                placeholder="empty = inherit from global" /></div>
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
