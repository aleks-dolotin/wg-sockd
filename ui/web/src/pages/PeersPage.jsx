import { useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { usePeers } from '@/api/hooks'
import { deletePeer, updatePeer } from '@/api/client'
import { formatBytes, truncateKey, isPeerOnline } from '@/lib/format'
import { useConnection } from '@/components/ConnectionContext'
import { usePeerFilters } from '@/hooks/usePeerFilters'
import { usePageTitle } from '@/hooks/usePageTitle'
import ApproveDialog from '@/components/ApproveDialog'
import { Button } from '@/components/ui/button'
import { Badge } from '@/components/ui/badge'
import { Input } from '@/components/ui/input'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table'
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { Skeleton } from '@/components/ui/skeleton'
import { toast } from 'sonner'

function PeersPageSkeleton() {
  return (
    <div className="space-y-4">
      <div className="flex items-center justify-between">
        <Skeleton className="h-8 w-32" />
        <Skeleton className="h-9 w-24" />
      </div>
      <div className="flex gap-2">
        <Skeleton className="h-9 flex-1" />
        <Skeleton className="h-9 w-28" />
        <Skeleton className="h-9 w-28" />
      </div>
      <div className="space-y-2">
        <Skeleton className="h-10 w-full" />
        {[...Array(5)].map((_, i) => <Skeleton key={i} className="h-12 w-full" />)}
      </div>
    </div>
  )
}

function SortableHead({ label, field, currentField, currentDir, onToggle }) {
  const active = currentField === field
  const arrow = active ? (currentDir === 'asc' ? ' ↑' : ' ↓') : ''
  return (
    <TableHead>
      <button
        className="hover:text-foreground transition-colors font-medium"
        onClick={() => onToggle(field)}
      >
        {label}{arrow}
      </button>
    </TableHead>
  )
}

export default function PeersPage() {
  usePageTitle('Peers')
  const navigate = useNavigate()
  const { data: peers, isLoading, error } = usePeers()
  const queryClient = useQueryClient()
  const [deleteTarget, setDeleteTarget] = useState(null)
  const [approveTarget, setApproveTarget] = useState(null)
  const [disableTarget, setDisableTarget] = useState(null)
  const { isConnected } = useConnection()

  const {
    query, statusFilter, profileFilter, autoFilter,
    sortField, sortDir,
    setQuery, setStatusFilter, setProfileFilter, setAutoFilter,
    toggleSort, filterAndSort,
  } = usePeerFilters()

  const deleteMut = useMutation({
    mutationFn: (id) => deletePeer(id),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['peers'] })
      setDeleteTarget(null)
      toast.success('Peer deleted')
    },
    onError: (err) => toast.error(err.message),
  })

  const toggleEnabledMut = useMutation({
    mutationFn: ({ id, enabled }) => updatePeer(id, { enabled }),
    onSuccess: (_, { enabled }) => {
      queryClient.invalidateQueries({ queryKey: ['peers'] })
      setDisableTarget(null)
      toast.success(enabled ? 'Peer enabled' : 'Peer disabled')
    },
    onError: (err) => toast.error(err.message),
  })

  if (isLoading) return <PeersPageSkeleton />
  if (error) return <p className="text-destructive">Error: {error.message}</p>

  const filteredPeers = filterAndSort(peers)
  const profileNames = [...new Set((peers || []).map(p => p.profile).filter(Boolean))]

  return (
    <div className="space-y-4">
      <div className="flex items-center justify-between">
        <h2 className="text-2xl font-semibold tracking-tight">Peers</h2>
        <Button onClick={() => navigate('/peers/new')} disabled={!isConnected}>Add Peer</Button>
      </div>

      {/* Search & Filters */}
      <div className="flex flex-wrap gap-2">
        <Input
          placeholder="Search by name or key…"
          value={query}
          onChange={(e) => setQuery(e.target.value)}
          className="flex-1 min-w-[200px]"
        />
        <select
          value={statusFilter}
          onChange={(e) => setStatusFilter(e.target.value)}
          className="rounded-md border border-input bg-background px-3 py-2 text-sm"
        >
          <option value="all">All statuses</option>
          <option value="online">Online</option>
          <option value="offline">Offline</option>
          <option value="disabled">Disabled</option>
        </select>
        <select
          value={profileFilter}
          onChange={(e) => setProfileFilter(e.target.value)}
          className="rounded-md border border-input bg-background px-3 py-2 text-sm"
        >
          <option value="all">All profiles</option>
          <option value="__none__">No profile</option>
          {profileNames.map(p => <option key={p} value={p}>{p}</option>)}
        </select>
        {autoFilter !== 'all' && (
          <Badge variant="outline" className="flex items-center gap-1 cursor-pointer" onClick={() => setAutoFilter('all')}>
            Auto-discovered × 
          </Badge>
        )}
      </div>

      {filteredPeers.length === 0 ? (
        <p className="text-muted-foreground">No peers match the current filters.</p>
      ) : (
        <>
          {/* Desktop table */}
          <div className="hidden md:block">
            <Table>
              <TableHeader>
                <TableRow>
                  <SortableHead label="Name" field="name" currentField={sortField} currentDir={sortDir} onToggle={toggleSort} />
                  <TableHead>Public Key</TableHead>
                  <TableHead>Client Routing</TableHead>
                  <SortableHead label="Profile" field="profile" currentField={sortField} currentDir={sortDir} onToggle={toggleSort} />
                  <SortableHead label="Status" field="status" currentField={sortField} currentDir={sortDir} onToggle={toggleSort} />
                  <SortableHead label="Transfer" field="transfer" currentField={sortField} currentDir={sortDir} onToggle={toggleSort} />
                  <TableHead className="text-right">Actions</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {filteredPeers.map(peer => {
                  const online = isPeerOnline(peer.latest_handshake)
                  return (
                    <TableRow key={peer.id} className={!peer.enabled ? 'opacity-50' : ''}>
                      <TableCell className="font-medium">
                        {peer.friendly_name || '—'}
                        {peer.auto_discovered && (
                          <Badge variant="secondary" className="ml-2 text-amber-600 dark:text-amber-400">Auto</Badge>
                        )}
                      </TableCell>
                      <TableCell className="font-mono text-xs">{truncateKey(peer.public_key)}</TableCell>
                      <TableCell className="text-xs max-w-[200px] truncate">{peer.client_allowed_ips || '—'}</TableCell>
                      <TableCell>{peer.profile || '—'}</TableCell>
                      <TableCell>
                        <Badge variant={online ? 'default' : 'secondary'}>{online ? 'Online' : 'Offline'}</Badge>
                        {!peer.enabled && <Badge variant="secondary" className="ml-1">Disabled</Badge>}
                      </TableCell>
                      <TableCell className="text-xs whitespace-nowrap">
                        ↓{formatBytes(peer.transfer_rx)} ↑{formatBytes(peer.transfer_tx)}
                      </TableCell>
                      <TableCell className="text-right space-x-1">
                        <Button variant="ghost" size="sm" onClick={() => navigate('/peers/' + peer.id)}>Edit</Button>
                        {peer.auto_discovered && !peer.enabled && (
                          <Button variant="outline" size="sm" disabled={!isConnected} onClick={() => setApproveTarget(peer)}>Approve</Button>
                        )}
                        {!peer.auto_discovered && (
                          <Button variant="ghost" size="sm" disabled={!isConnected}
                            onClick={() => peer.enabled ? setDisableTarget(peer) : toggleEnabledMut.mutate({ id: peer.id, enabled: true })}>
                            {peer.enabled ? 'Disable' : 'Enable'}
                          </Button>
                        )}
                        <Button variant="ghost" size="sm" className="text-destructive" disabled={!isConnected} onClick={() => setDeleteTarget(peer)}>Delete</Button>
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
                      <CardTitle className="text-base">{peer.friendly_name || truncateKey(peer.public_key)}</CardTitle>
                      <div className="flex gap-1">
                        <Badge variant={online ? 'default' : 'secondary'}>{online ? 'Online' : 'Offline'}</Badge>
                        {!peer.enabled && <Badge variant="secondary">Disabled</Badge>}
                      </div>
                    </div>
                  </CardHeader>
                  <CardContent className="space-y-2 text-sm">
                    {peer.auto_discovered && <Badge variant="secondary" className="text-amber-600 dark:text-amber-400">Auto-discovered</Badge>}
                    <p className="text-muted-foreground truncate">{peer.client_allowed_ips || '—'}</p>
                    <p className="text-xs text-muted-foreground">↓{formatBytes(peer.transfer_rx)} ↑{formatBytes(peer.transfer_tx)}</p>
                    <div className="flex gap-2 pt-1">
                      <Button variant="outline" size="sm" onClick={() => navigate('/peers/' + peer.id)}>Edit</Button>
                      {peer.auto_discovered && !peer.enabled && (
                        <Button variant="outline" size="sm" disabled={!isConnected} onClick={() => setApproveTarget(peer)}>Approve</Button>
                      )}
                      {!peer.auto_discovered && (
                        <Button variant="outline" size="sm" disabled={!isConnected}
                          onClick={() => peer.enabled ? setDisableTarget(peer) : toggleEnabledMut.mutate({ id: peer.id, enabled: true })}>
                          {peer.enabled ? 'Disable' : 'Enable'}
                        </Button>
                      )}
                      <Button variant="outline" size="sm" className="text-destructive" disabled={!isConnected} onClick={() => setDeleteTarget(peer)}>Delete</Button>
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
              Are you sure you want to delete &quot;{deleteTarget?.friendly_name || truncateKey(deleteTarget?.public_key)}&quot;? This action cannot be undone.
            </DialogDescription>
          </DialogHeader>
          <DialogFooter>
            <Button variant="outline" onClick={() => setDeleteTarget(null)}>Cancel</Button>
            <Button variant="destructive" onClick={() => deleteMut.mutate(deleteTarget?.id)} disabled={deleteMut.isPending || !isConnected}>
              {deleteMut.isPending ? 'Deleting...' : 'Delete'}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      {/* Disable confirmation dialog */}
      <Dialog open={!!disableTarget} onOpenChange={() => setDisableTarget(null)}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Disable Peer</DialogTitle>
            <DialogDescription>
              Peer &quot;{disableTarget?.friendly_name || truncateKey(disableTarget?.public_key)}&quot; will be disconnected. You can re-enable it later.
            </DialogDescription>
          </DialogHeader>
          <DialogFooter>
            <Button variant="outline" onClick={() => setDisableTarget(null)}>Cancel</Button>
            <Button onClick={() => toggleEnabledMut.mutate({ id: disableTarget?.id, enabled: false })} disabled={toggleEnabledMut.isPending || !isConnected}>
              {toggleEnabledMut.isPending ? 'Disabling…' : 'Disable'}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      {/* Approve onboarding dialog */}
      <ApproveDialog
        peer={approveTarget}
        open={!!approveTarget}
        onClose={() => setApproveTarget(null)}
      />

    </div>
  )
}
