import { useNavigate } from 'react-router-dom'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { createProfile } from '@/api/client'
import { usePageTitle } from '@/hooks/usePageTitle'
import ProfileForm from '@/components/ProfileForm'
import { toast } from 'sonner'

export default function ProfileNewPage() {
  usePageTitle('Create Profile')
  const navigate = useNavigate()
  const queryClient = useQueryClient()

  const createMut = useMutation({
    mutationFn: (payload) => createProfile(payload),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['profiles'] })
      toast.success('Profile created')
      navigate('/settings/profiles')
    },
    onError: (err) => toast.error(err.message),
  })

  return (
    <div className="space-y-4">
      <h2 className="text-2xl font-semibold tracking-tight">Create Profile</h2>
      <ProfileForm
        isNew
        onSubmit={(payload) => createMut.mutate(payload)}
        isPending={createMut.isPending}
        error={createMut.error}
        onCancel={() => navigate('/settings/profiles')}
      />
    </div>
  )
}
