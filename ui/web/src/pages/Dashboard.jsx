import { useState } from 'react'
import { usePeers, useStats } from '@/api/hooks'
import { formatBytes, isPeerOnline } from '@/lib/format'
import { usePageTitle } from '@/hooks/usePageTitle'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Skeleton } from '@/components/ui/skeleton'

const TOP_N = 20

function DashboardSkeleton() {
  return (
    <div className="space-y-6">
      <Skeleton className="h-8 w-40" />
      <div className="grid grid-cols-2 md:grid-cols-4 gap-3">
        {[...Array(4)].map((_, i) => (
          <Card key={i}><CardContent className="pt-6"><Skeleton className="h-8 w-20" /></CardContent></Card>
        ))}
      </div>
      <Card>
        <CardContent className="pt-6 space-y-3">
          {[...Array(5)].map((_, i) => (
            <div key={i} className="space-y-1">
              <Skeleton className="h-4 w-48" />
              <Skeleton className="h-2 w-full" />
            </div>
          ))}
        </CardContent>
      </Card>
    </div>
  )
}

export default function Dashboard() {
  usePageTitle('Dashboard')
  const { data: stats, isLoading: statsLoading } = useStats()
  const { data: peers, isLoading: peersLoading } = usePeers()
  const [showAll, setShowAll] = useState(false)

  if (statsLoading || peersLoading) return <DashboardSkeleton />

  const onlinePeers = (peers || []).filter(p => isPeerOnline(p.latest_handshake))
  const totalPeers = stats?.total_peers || peers?.length || 0
  const onlineCount = stats?.online_peers ?? onlinePeers.length

  const sortedPeers = [...(peers || [])].sort((a, b) =>
    ((b.transfer_rx || 0) + (b.transfer_tx || 0)) - ((a.transfer_rx || 0) + (a.transfer_tx || 0))
  )
  const maxTransfer = sortedPeers.length > 0
    ? (sortedPeers[0].transfer_rx || 0) + (sortedPeers[0].transfer_tx || 0) : 1

  const visiblePeers = showAll ? sortedPeers : sortedPeers.slice(0, TOP_N)
  const hasMore = sortedPeers.length > TOP_N

  return (
    <div className="space-y-6">
      <h2 className="text-2xl font-semibold tracking-tight">Dashboard</h2>

      <div className="grid grid-cols-2 md:grid-cols-4 gap-3">
        <Card>
          <CardHeader className="pb-1"><CardTitle className="text-sm text-muted-foreground">Total Peers</CardTitle></CardHeader>
          <CardContent><p className="text-2xl font-bold">{totalPeers}</p></CardContent>
        </Card>
        <Card>
          <CardHeader className="pb-1"><CardTitle className="text-sm text-muted-foreground">Online</CardTitle></CardHeader>
          <CardContent><p className="text-2xl font-bold text-green-600 dark:text-green-400">
            {onlineCount} <span className="text-sm text-muted-foreground">{totalPeers > 0 ? `(${Math.round(onlineCount / totalPeers * 100)}%)` : ''}</span>
          </p></CardContent>
        </Card>
        <Card>
          <CardHeader className="pb-1"><CardTitle className="text-sm text-muted-foreground">Total RX</CardTitle></CardHeader>
          <CardContent><p className="text-2xl font-bold">{formatBytes(stats?.total_transfer_rx || 0)}</p></CardContent>
        </Card>
        <Card>
          <CardHeader className="pb-1"><CardTitle className="text-sm text-muted-foreground">Total TX</CardTitle></CardHeader>
          <CardContent><p className="text-2xl font-bold">{formatBytes(stats?.total_transfer_tx || 0)}</p></CardContent>
        </Card>
      </div>

      <Card>
        <CardHeader><CardTitle className="text-base">Per-Peer Transfer</CardTitle></CardHeader>
        <CardContent className="space-y-2">
          {sortedPeers.length === 0 ? (
            <p className="text-muted-foreground text-sm">No peers yet.</p>
          ) : (
            <>
              {visiblePeers.map(peer => {
                const total = (peer.transfer_rx || 0) + (peer.transfer_tx || 0)
                const pct = Math.max((total / maxTransfer) * 100, 2)
                const online = isPeerOnline(peer.latest_handshake)
                return (
                  <div key={peer.id} className="space-y-1">
                    <div className="flex items-center justify-between text-sm">
                      <span className="truncate">{peer.friendly_name || peer.public_key?.slice(0, 12)}</span>
                      <div className="flex items-center gap-2">
                        <Badge variant={online ? 'default' : 'secondary'} className="text-xs">
                          {online ? 'Online' : 'Offline'}
                        </Badge>
                        <span className="text-xs text-muted-foreground w-20 text-right">{formatBytes(total)}</span>
                      </div>
                    </div>
                    <div className="h-2 bg-muted rounded-full overflow-hidden">
                      <div
                        className={`h-full rounded-full ${online ? 'bg-green-500 dark:bg-green-400' : 'bg-muted-foreground/30'}`}
                        style={{ width: pct + '%' }}
                      />
                    </div>
                  </div>
                )
              })}
              {hasMore && (
                <Button variant="ghost" size="sm" className="w-full mt-2" onClick={() => setShowAll(!showAll)}>
                  {showAll ? 'Show top 20' : `Show all (${sortedPeers.length} peers)`}
                </Button>
              )}
            </>
          )}
        </CardContent>
      </Card>
    </div>
  )
}
