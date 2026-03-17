import { useParams, useNavigate } from 'react-router-dom'
import { usePeer } from '@/api/hooks'
import { usePageTitle } from '@/hooks/usePageTitle'
import PeerStatusCard from '@/components/PeerStatusCard'
import PeerActionsBar from '@/components/PeerActionsBar'
import PeerEditForm from '@/components/PeerEditForm'
import { Button } from '@/components/ui/button'
import { Card, CardContent } from '@/components/ui/card'
import { Skeleton } from '@/components/ui/skeleton'

function PeerDetailSkeleton() {
  return (
    <div className="max-w-2xl space-y-6">
      <div className="flex items-center justify-between">
        <Skeleton className="h-8 w-48" />
        <Skeleton className="h-9 w-20" />
      </div>
      <Card>
        <CardContent className="pt-6 space-y-4">
          <Skeleton className="h-4 w-full" />
          <div className="grid grid-cols-2 gap-4">
            <Skeleton className="h-4 w-32" />
            <Skeleton className="h-4 w-32" />
            <Skeleton className="h-4 w-24" />
            <Skeleton className="h-4 w-24" />
          </div>
        </CardContent>
      </Card>
      <div className="flex gap-2">
        <Skeleton className="h-9 w-24" />
        <Skeleton className="h-9 w-28" />
        <Skeleton className="h-9 w-32" />
      </div>
      <Card>
        <CardContent className="pt-6 space-y-4">
          <Skeleton className="h-9 w-full" />
          <Skeleton className="h-9 w-full" />
          <Skeleton className="h-9 w-full" />
        </CardContent>
      </Card>
    </div>
  )
}

export default function PeerDetailPage() {
  const { id } = useParams()
  const navigate = useNavigate()
  const { data: peer, isLoading, error } = usePeer(id)

  usePageTitle(peer?.friendly_name || 'Peer')

  if (isLoading) return <PeerDetailSkeleton />

  // Graceful 404 — peer deleted externally
  if (error?.status === 404 || (!isLoading && !peer)) {
    return (
      <div className="max-w-2xl space-y-6">
        <Card>
          <CardContent className="pt-6 text-center space-y-4">
            <p className="text-lg font-medium">This peer has been deleted</p>
            <p className="text-sm text-muted-foreground">
              The peer may have been removed from another session or via the CLI.
            </p>
            <Button onClick={() => navigate('/peers')}>Back to Peers</Button>
          </CardContent>
        </Card>
      </div>
    )
  }

  if (error) {
    return (
      <div className="max-w-2xl">
        <Card>
          <CardContent className="pt-6 text-center space-y-4">
            <p className="text-destructive">Error: {error.message}</p>
            <Button variant="outline" onClick={() => navigate('/peers')}>Back to Peers</Button>
          </CardContent>
        </Card>
      </div>
    )
  }

  return (
    <div className="max-w-2xl space-y-6">
      <div className="flex items-center justify-between">
        <h2 className="text-2xl font-semibold tracking-tight">
          {peer.friendly_name || 'Unnamed Peer'}
        </h2>
        <Button variant="outline" onClick={() => navigate('/peers')}>Back</Button>
      </div>

      <PeerStatusCard peer={peer} />
      <PeerActionsBar peer={peer} />
      <PeerEditForm peer={peer} />
    </div>
  )
}
