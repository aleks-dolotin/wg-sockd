import { useState } from 'react'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { formatBytes, isPeerOnline, formatRelativeTime } from '@/lib/format'

export default function PeerStatusCard({ peer }) {
  const [copied, setCopied] = useState(false)
  const online = isPeerOnline(peer.latest_handshake)

  const copyKey = async () => {
    try {
      await navigator.clipboard.writeText(peer.public_key)
      setCopied(true)
      setTimeout(() => setCopied(false), 2000)
    } catch { /* ignore */ }
  }

  return (
    <Card>
      <CardHeader className="pb-3">
        <CardTitle className="flex items-center justify-between">
          <span>Peer Info</span>
          <div className="flex gap-2">
            <Badge variant={online ? 'default' : 'secondary'}>
              {online ? 'Online' : 'Offline'}
            </Badge>
            <Badge variant={peer.enabled ? 'default' : 'secondary'}>
              {peer.enabled ? 'Enabled' : 'Disabled'}
            </Badge>
            {peer.auto_discovered && (
              <Badge variant="outline" className="text-amber-600 border-amber-300 dark:text-amber-400 dark:border-amber-600">
                Auto-discovered
              </Badge>
            )}
          </div>
        </CardTitle>
      </CardHeader>
      <CardContent>
        <dl className="grid grid-cols-1 sm:grid-cols-2 gap-x-6 gap-y-3 text-sm">
          <div className="sm:col-span-2">
            <dt className="text-muted-foreground mb-0.5">Public Key</dt>
            <dd className="flex items-center gap-2">
              <code className="font-mono text-xs bg-muted px-2 py-1 rounded break-all">{peer.public_key}</code>
              <Button variant="ghost" size="sm" className="h-7 px-2 shrink-0" onClick={copyKey}>
                {copied ? 'Copied!' : 'Copy'}
              </Button>
            </dd>
          </div>
          <div>
            <dt className="text-muted-foreground mb-0.5">Endpoint</dt>
            <dd className="font-mono text-xs">{peer.endpoint || '—'}</dd>
          </div>
          <div>
            <dt className="text-muted-foreground mb-0.5">Last Handshake</dt>
            <dd>{formatRelativeTime(peer.latest_handshake)}</dd>
          </div>
          <div>
            <dt className="text-muted-foreground mb-0.5">Transfer RX</dt>
            <dd>{formatBytes(peer.transfer_rx)}</dd>
          </div>
          <div>
            <dt className="text-muted-foreground mb-0.5">Transfer TX</dt>
            <dd>{formatBytes(peer.transfer_tx)}</dd>
          </div>
          <div>
            <dt className="text-muted-foreground mb-0.5">Profile</dt>
            <dd>{peer.profile || '—'}</dd>
          </div>
          <div>
            <dt className="text-muted-foreground mb-0.5">Allowed IPs</dt>
            <dd className="font-mono text-xs">{peer.allowed_ips?.join(', ') || '—'}</dd>
          </div>
        </dl>
      </CardContent>
    </Card>
  )
}
