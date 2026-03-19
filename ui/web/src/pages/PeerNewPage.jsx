import { useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { createPeer } from '@/api/client'
import { usePageTitle } from '@/hooks/usePageTitle'
import PeerForm from '@/components/PeerForm'
import { Button } from '@/components/ui/button'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { Dialog, DialogContent, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { toast } from 'sonner'

export default function PeerNewPage() {
  usePageTitle('Add Peer')
  const navigate = useNavigate()
  const queryClient = useQueryClient()
  const [result, setResult] = useState(null)
  const [peerName, setPeerName] = useState('')

  const createMut = useMutation({
    mutationFn: (data) => createPeer(data),
    onSuccess: (data) => {
      queryClient.invalidateQueries({ queryKey: ['peers'] })
      setPeerName(data.friendly_name)
      setResult(data)
    },
    onError: (err) => toast.error(err.message),
  })

  return (
    <div className="space-y-4 max-w-lg mx-auto">
      <h2 className="text-2xl font-semibold tracking-tight">Add Peer</h2>
      {!result ? (
        <PeerForm
          mode="create"
          onSubmit={(body) => createMut.mutate(body)}
          isPending={createMut.isPending}
          error={createMut.error}
          onCancel={() => navigate('/peers')}
        />
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
              <Button onClick={() => { toast.success(`Peer ${peerName} created`); navigate('/peers') }}>Done</Button>
            </DialogFooter>
          </DialogContent>
        </Dialog>
      )}
    </div>
  )
}
