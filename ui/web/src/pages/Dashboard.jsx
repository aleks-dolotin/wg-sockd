import { usePeers, useStats } from '@/api/hooks'
import { formatBytes, isPeerOnline } from '@/lib/format'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Badge } from '@/components/ui/badge'

export default function Dashboard() {
  const { data: stats, isLoading: statsLoading } = useStats()
  const { data: peers, isLoading: peersLoading } = usePeers()

  if (statsLoading || peersLoading) return <p className="text-gray-500">Loading dashboard...</p>

  const onlinePeers = (peers || []).filter(p => isPeerOnline(p.latest_handshake))
  const totalPeers = stats?.total_peers || peers?.length || 0
  const onlineCount = stats?.online_peers ?? onlinePeers.length

  // Sort peers by total transfer descending
  const sortedPeers = [...(peers || [])].sort((a, b) =>
    ((b.transfer_rx || 0) + (b.transfer_tx || 0)) - ((a.transfer_rx || 0) + (a.transfer_tx || 0))
  )
  const maxTransfer = sortedPeers.length > 0
    ? (sortedPeers[0].transfer_rx || 0) + (sortedPeers[0].transfer_tx || 0) : 1

  return (
    <div className="space-y-6">
      <h2 className="text-2xl font-semibold tracking-tight">Dashboard</h2>

      {/* Summary cards */}
      <div className="grid grid-cols-2 md:grid-cols-4 gap-3">
        <Card>
          <CardHeader className="pb-1"><CardTitle className="text-sm text-gray-500">Total Peers</CardTitle></CardHeader>
          <CardContent><p className="text-2xl font-bold">{totalPeers}</p></CardContent>
        </Card>
        <Card>
          <CardHeader className="pb-1"><CardTitle className="text-sm text-gray-500">Online</CardTitle></CardHeader>
          <CardContent><p className="text-2xl font-bold text-green-600">
            {onlineCount} <span className="text-sm text-gray-400">{totalPeers > 0 ? `(${Math.round(onlineCount / totalPeers * 100)}%)` : ''}</span>
          </p></CardContent>
        </Card>
        <Card>
          <CardHeader className="pb-1"><CardTitle className="text-sm text-gray-500">Total RX</CardTitle></CardHeader>
          <CardContent><p className="text-2xl font-bold">{formatBytes(stats?.total_transfer_rx || 0)}</p></CardContent>
        </Card>
        <Card>
          <CardHeader className="pb-1"><CardTitle className="text-sm text-gray-500">Total TX</CardTitle></CardHeader>
          <CardContent><p className="text-2xl font-bold">{formatBytes(stats?.total_transfer_tx || 0)}</p></CardContent>
        </Card>
      </div>

      {/* Per-peer transfer bars */}
      <Card>
        <CardHeader><CardTitle className="text-base">Per-Peer Transfer</CardTitle></CardHeader>
        <CardContent className="space-y-2">
          {sortedPeers.length === 0 ? (
            <p className="text-gray-500 text-sm">No peers yet.</p>
          ) : sortedPeers.map(peer => {
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
                    <span className="text-xs text-gray-500 w-20 text-right">{formatBytes(total)}</span>
                  </div>
                </div>
                <div className="h-2 bg-gray-100 rounded-full overflow-hidden">
                  <div
                    className={`h-full rounded-full ${online ? 'bg-green-500' : 'bg-gray-400'}`}
                    style={{ width: pct + '%' }}
                  />
                </div>
              </div>
            )
          })}
        </CardContent>
      </Card>
    </div>
  )
}

