import { useMutation, useQueryClient } from '@tanstack/react-query'
import { updatePeer } from '@/api/client'
import PeerForm from '@/components/PeerForm'
import { peerToFormData } from '@/lib/peer-form-utils'
import { toast } from 'sonner'

export default function PeerEditForm({ peer }) {
  const queryClient = useQueryClient()

  const saveMut = useMutation({
    mutationFn: (update) => updatePeer(peer.id, update),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['peers'] })
      queryClient.invalidateQueries({ queryKey: ['peers', String(peer.id)] })
      toast.success('Changes saved')
    },
    onError: (err) => toast.error(err.message),
  })

  return (
    <PeerForm
      mode="edit"
      initialData={peerToFormData(peer)}
      peer={peer}
      onSubmit={(body) => saveMut.mutate(body)}
      isPending={saveMut.isPending}
      error={saveMut.error}
      onCancel={() => {}} // no-op — edit is inline on detail page
    />
  )
}
