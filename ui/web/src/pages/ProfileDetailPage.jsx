import { useParams, useNavigate } from 'react-router-dom'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { useProfile } from '@/api/hooks'
import { updateProfile, deleteProfile } from '@/api/client'
import { usePageTitle } from '@/hooks/usePageTitle'
import ProfileForm from '@/components/ProfileForm'
import { profileToFormData } from '@/lib/profile-form-utils'
import { Button } from '@/components/ui/button'
import { Badge } from '@/components/ui/badge'
import { Card, CardContent } from '@/components/ui/card'
import { Skeleton } from '@/components/ui/skeleton'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { useState } from 'react'
import { toast } from 'sonner'

function ProfileDetailSkeleton() {
  return (
    <div className="max-w-lg space-y-6">
      <Skeleton className="h-8 w-48" />
      <Card><CardContent className="pt-6 space-y-4">
        <Skeleton className="h-4 w-full" />
        <Skeleton className="h-4 w-64" />
        <Skeleton className="h-4 w-48" />
      </CardContent></Card>
    </div>
  )
}

export default function ProfileDetailPage() {
  const { name } = useParams()
  const navigate = useNavigate()
  const queryClient = useQueryClient()
  const { data: profile, isLoading, error } = useProfile(name)
  usePageTitle(profile ? `Profile: ${profile.name}` : 'Profile')

  const [showDelete, setShowDelete] = useState(false)
  const [deleteError, setDeleteError] = useState(null)

  const updateMut = useMutation({
    mutationFn: (payload) => updateProfile(name, payload),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['profiles'] })
      queryClient.invalidateQueries({ queryKey: ['profiles', name] })
      toast.success('Profile updated')
      navigate('/settings/profiles')
    },
    onError: (err) => toast.error(err.message),
  })

  const deleteMut = useMutation({
    mutationFn: () => deleteProfile(name),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['profiles'] })
      toast.success('Profile deleted')
      navigate('/settings/profiles')
    },
    onError: (err) => setDeleteError(err.message),
  })

  if (isLoading) return <ProfileDetailSkeleton />
  if (error) return <p className="text-destructive">Error: {error.message}</p>
  if (!profile) return <p className="text-destructive">Profile not found</p>

  return (
    <div className="space-y-4">
      <div className="flex items-center justify-between">
        <div className="flex items-center gap-3">
          <h2 className="text-2xl font-semibold tracking-tight">{profile.name}</h2>
          {profile.is_default && <Badge variant="secondary">Default</Badge>}
        </div>
        <Button variant="destructive" size="sm" onClick={() => { setShowDelete(true); setDeleteError(null) }}>
          Delete
        </Button>
      </div>

      <div className="flex gap-4 text-sm text-muted-foreground">
        <span>{profile.resolved_allowed_ips?.length || 0} resolved routes</span>
        <span>{profile.peer_count || 0} peers</span>
      </div>

      <ProfileForm
        initialData={profileToFormData(profile)}
        onSubmit={(payload) => updateMut.mutate(payload)}
        isPending={updateMut.isPending}
        error={updateMut.error}
        onCancel={() => navigate('/settings/profiles')}
      />

      {/* Delete confirmation dialog */}
      <Dialog open={showDelete} onOpenChange={() => { setShowDelete(false); setDeleteError(null) }}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Delete Profile</DialogTitle>
            <DialogDescription>
              Are you sure you want to delete profile &quot;{name}&quot;? This action cannot be undone.
            </DialogDescription>
          </DialogHeader>
          {deleteError && <Alert variant="destructive"><AlertDescription>{deleteError}</AlertDescription></Alert>}
          <DialogFooter>
            <Button variant="outline" onClick={() => { setShowDelete(false); setDeleteError(null) }}>Cancel</Button>
            <Button variant="destructive" onClick={() => deleteMut.mutate()} disabled={deleteMut.isPending}>
              {deleteMut.isPending ? 'Deleting...' : 'Delete'}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  )
}
