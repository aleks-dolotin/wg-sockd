import { useState } from 'react'
import { useNavigate, useSearchParams } from 'react-router-dom'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { usePeers } from '@/api/hooks'
import { deletePeer, approvePeer } from '@/api/client'
import { formatBytes, truncateKey, isPeerOnline } from '@/lib/format'
import { Button } from '@/components/ui/button'
import { Badge } from '@/components/ui/badge'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table'
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog'

export default function PeersPage() {
  const navigate = useNavigate()
  const [searchParams] = useSearchParams()
  const filterAuto = searchParams.get('filter') === 'auto_discovered'
  const { data: peers, isLoading, error } = usePeers()
  const queryClient = useQueryClient()
  const [deleteTarget, setDeleteTarget] = useState(null)
  const [qrPeer, setQrPeer] = useState(null)

  const deleteMut = useMutation({
    mutationFn: (id) => deletePeer(id),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['peers'] })
      setDeleteTarget(null)
    },
  })

  const approveMut = useMutation({
    mutationFn: (id) => approvePeer(id),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ['peers'] }),
  })

  if (isLoading) {
    return <p className="text-gray-500">Loading peers...</p>
  }
  if (error) {
    return <p className="text-red-500">Error: {error.message}</p>
  }

  const filteredPeers = filterAuto
    ? (peers || []).filter(p => p.auto_discovered)
    : (peers || [])

  return (
    <div className="space-y-4">
      <div className="flex items-center justify-between">
        <h2 className="text-2xl font-semibold tracking-tight">
          Peers
          {filterAuto && (
            <Badge variant="outline" className="ml-2">Auto-discovered only</Badge>
          )}
        </h2>
        <Button onClick={() => navigate('/peers/new')}>Add Peer</Button>
      </div>

      {filteredPeers.length === 0 ? (
        <p className="text-gray-500">No peers found.</p>
      ) : (
        <>
          {/* Desktop table */}
          <div className="hidden md:block">
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>Name</TableHead>
                  <TableHead>Public Key</TableHead>
                  <TableHead>Allowed IPs</TableHead>
                  <TableHead>Profile</TableHead>
                  <TableHead>Status</TableHead>
                  <TableHead>Transfer</TableHead>
                  <TableHead className="text-right">Actions</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {filteredPeers.map(peer => {
                  const online = isPeerOnline(peer.latest_handshake)
                  return (
                    <TableRow key={peer.id}>
                      <TableCell className="font-medium">
                        {peer.friendly_name || '\u2014'}
                        {peer.auto_discovered && (
                          <Badge variant="secondary" className="ml-2 text-orange-600">Auto</Badge>
                        )}
                      </TableCell>
                      <TableCell className="font-mono text-xs">
                        {truncateKey(peer.public_key)}
                      </TableCell>
                      <TableCell className="text-xs">
                        {peer.allowed_ips?.join(', ') || '\u2014'}
                      </TableCell>
                      <TableCell>{peer.profile || '\u2014'}</TableCell>
                      <TableCell>
                        <Badge variant={online ? 'default' : 'secondary'}>
                          {online ? 'Online' : 'Offline'}
                        </Badge>
                      </TableCell>
                      <TableCell className="text-xs">
                        {'\u2193'}{formatBytes(peer.transfer_rx)}{' '}
                        {'\u2191'}{formatBytes(peer.transfer_tx)}
                      </TableCell>
                      <TableCell className="text-right space-x-1">
                        <Button variant="ghost" size="sm"
                          onClick={() => navigate('/peers/' + peer.id)}>Edit</Button>
                        <Button variant="ghost" size="sm"
                          onClick={() => setQrPeer(peer)}>QR</Button>
                        {peer.auto_discovered && (
                          <Button variant="outline" size="sm"
                            onClick={() => approveMut.mutate(peer.id)}>Approve</Button>
                        )}
                        <Button variant="ghost" size="sm" className="text-red-600"
                          onClick={() => setDeleteTarget(peer)}>Delete</Button>
                      </TableCell>
                    </TableRow>
                  )
                })}
              </TableBody>
            </Table>
          </div>

          {/* Mobile cards */}
          <div className="md:hidden grid gap-3">
            {filteredPeers.map(peer => {
              const online = isPeerOnline(peer.latest_handshake)
              return (
                <Card key={peer.id}>
                  <CardHeader className="pb-2">
                    <div className="flex items-center justify-between">
                      <CardTitle className="text-base">
                        {peer.friendly_name || truncateKey(peer.public_key)}
                      </CardTitle>
                      <Badge variant={online ? 'default' : 'secondary'}>
                        {online ? 'Online' : 'Offline'}
                      </Badge>
                    </div>
                  </CardHeader>
                  <CardContent className="space-y-2 text-sm">
                    {peer.auto_discovered && (
                      <Badge variant="secondary" className="text-orange-600">Auto-discovered</Badge>
                    )}
                    <p className="text-gray-600">{peer.allowed_ips?.join(', ')}</p>
                    <p className="text-xs text-gray-400">
                      {'\u2193'}{formatBytes(peer.transfer_rx)}{' '}
                      {'\u2191'}{formatBytes(peer.transfer_tx)}
                    </p>
                    <div className="flex gap-2 pt-1">
                      <Button variant="outline" size="sm"
                        onClick={() => navigate('/peers/' + peer.id)}>Edit</Button>
                      <Button variant="outline" size="sm"
                        onClick={() => setQrPeer(peer)}>QR</Button>
                      {peer.auto_discovered && (
                        <Button variant="outline" size="sm"
                          onClick={() => approveMut.mutate(peer.id)}>Approve</Button>
                      )}
                      <Button variant="outline" size="sm" className="text-red-600"
                        onClick={() => setDeleteTarget(peer)}>Delete</Button>
                    </div>
                  </CardContent>
                </Card>
              )
            })}
          </div>
        </>
      )}

      {/* Delete confirmation dialog */}
      <Dialog open={!!deleteTarget} onOpenChange={() => setDeleteTarget(null)}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Delete Peer</DialogTitle>
            <DialogDescription>
              Are you sure you want to delete &quot;{deleteTarget?.friendly_name || truncateKey(deleteTarget?.public_key)}&quot;?
              This action cannot be undone.
            </DialogDescription>
          </DialogHeader>
          <DialogFooter>
            <Button variant="outline" onClick={() => setDeleteTarget(null)}>Cancel</Button>
            <Button variant="destructive"
              onClick={() => deleteMut.mutate(deleteTarget?.id)}
              disabled={deleteMut.isPending}>
              {deleteMut.isPending ? 'Deleting...' : 'Delete'}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      {/* QR dialog */}
      <Dialog open={!!qrPeer} onOpenChange={() => setQrPeer(null)}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>QR Code</DialogTitle>
          </DialogHeader>
          <div className="flex justify-center p-4">
            <img
              src={'/api/peers/' + qrPeer?.id + '/qr'}
              alt="QR Code"
              className="w-64 h-64"
            />
          </div>
          <DialogFooter>
            <Button variant="outline" asChild>
              <a href={'/api/peers/' + qrPeer?.id + '/conf'} download>
                Download .conf
              </a>
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  )
}

