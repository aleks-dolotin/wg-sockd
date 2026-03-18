import { useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { updatePeer, deletePeer, rotatePeerKeys, approvePeer } from '@/api/client'
import { useConnection } from '@/components/ConnectionContext'
import { Button } from '@/components/ui/button'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { Dialog, DialogContent, DialogFooter, DialogHeader, DialogTitle, DialogDescription } from '@/components/ui/dialog'
import { toast } from 'sonner'

export default function PeerActionsBar({ peer }) {
  const navigate = useNavigate()
  const queryClient = useQueryClient()
  const { isConnected } = useConnection()
  const [deleteDialogOpen, setDeleteDialogOpen] = useState(false)
  const [rotateDialogOpen, setRotateDialogOpen] = useState(false)
  const [rotateConfirmOpen, setRotateConfirmOpen] = useState(false)
  const [rotateResult, setRotateResult] = useState(null)

  const invalidate = () => {
    queryClient.invalidateQueries({ queryKey: ['peers'] })
    queryClient.invalidateQueries({ queryKey: ['peers', String(peer.id)] })
  }

  const toggleMut = useMutation({
    mutationFn: () => updatePeer(peer.id, { enabled: !peer.enabled }),
    onSuccess: () => { invalidate(); toast.success(`Peer ${peer.enabled ? 'disabled' : 'enabled'}`) },
    onError: (err) => toast.error(err.message),
  })

  const deleteMut = useMutation({
    mutationFn: () => deletePeer(peer.id),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['peers'] })
      toast.success('Peer deleted')
      navigate('/peers')
    },
    onError: (err) => toast.error(err.message),
  })

  const rotateMut = useMutation({
    mutationFn: () => rotatePeerKeys(peer.id),
    onSuccess: (data) => {
      invalidate()
      setRotateConfirmOpen(false)
      setRotateResult(data)
      setRotateDialogOpen(true)
    },
    onError: (err) => { toast.error(err.message); setRotateConfirmOpen(false) },
  })

  const approveMut = useMutation({
    mutationFn: () => approvePeer(peer.id),
    onSuccess: () => { invalidate(); toast.success('Peer approved') },
    onError: (err) => toast.error(err.message),
  })

  return (
    <>
      <div className="flex flex-wrap gap-2">
        {peer.auto_discovered && !peer.enabled && (
          <Button onClick={() => approveMut.mutate()} disabled={!isConnected || approveMut.isPending}>
            {approveMut.isPending ? 'Approving…' : 'Approve'}
          </Button>
        )}
        <Button
          variant="outline"
          onClick={() => toggleMut.mutate()}
          disabled={!isConnected || toggleMut.isPending}
        >
          {peer.enabled ? 'Disable' : 'Enable'}
        </Button>
        <Button
          variant="outline"
          onClick={() => setRotateConfirmOpen(true)}
          disabled={!isConnected}
        >
          Rotate Keys
        </Button>
        <Button variant="destructive" onClick={() => setDeleteDialogOpen(true)} disabled={!isConnected}>
          Delete
        </Button>
      </div>

      {/* Delete confirmation */}
      <Dialog open={deleteDialogOpen} onOpenChange={setDeleteDialogOpen}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Delete Peer</DialogTitle>
            <DialogDescription>
              This will permanently remove <strong>{peer.friendly_name || 'this peer'}</strong> and its WireGuard configuration.
            </DialogDescription>
          </DialogHeader>
          <DialogFooter>
            <Button variant="outline" onClick={() => setDeleteDialogOpen(false)}>Cancel</Button>
            <Button variant="destructive" onClick={() => deleteMut.mutate()} disabled={deleteMut.isPending}>
              {deleteMut.isPending ? 'Deleting…' : 'Delete'}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      {/* Rotate keys confirmation */}
      <Dialog open={rotateConfirmOpen} onOpenChange={setRotateConfirmOpen}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Rotate Keys</DialogTitle>
            <DialogDescription>
              This will generate new keys for <strong>{peer.friendly_name || 'this peer'}</strong>. The old keys will stop working immediately.
            </DialogDescription>
          </DialogHeader>
          <DialogFooter>
            <Button variant="outline" onClick={() => setRotateConfirmOpen(false)}>Cancel</Button>
            <Button onClick={() => rotateMut.mutate()} disabled={rotateMut.isPending}>
              {rotateMut.isPending ? 'Rotating…' : 'Rotate Keys'}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      {/* Rotate result — new QR + conf */}
      <Dialog open={rotateDialogOpen} onOpenChange={(open) => {
        if (!open) { setRotateDialogOpen(false); setRotateResult(null) }
      }}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>New Keys Generated</DialogTitle>
          </DialogHeader>
          <Alert>
            <AlertDescription>Save this configuration now — the private key will not be shown again.</AlertDescription>
          </Alert>
          <div className="flex justify-center p-4">
            <img src={`data:image/png;base64,${rotateResult?.qr}`} alt="QR Code" className="w-64 h-64" />
          </div>
          <DialogFooter>
            <Button variant="outline" onClick={() => {
              if (!rotateResult?.config) return
              const blob = new Blob([rotateResult.config], { type: 'text/plain' })
              const url = URL.createObjectURL(blob)
              const a = document.createElement('a')
              a.href = url
              a.download = `${peer.friendly_name || 'peer'}.conf`
              a.click()
              URL.revokeObjectURL(url)
            }}>Download .conf</Button>
            <Button onClick={() => { toast.success(`Keys rotated for ${peer.friendly_name || 'peer'}`); setRotateDialogOpen(false); setRotateResult(null) }}>
              Done
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </>
  )
}
