import { useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { usePeers } from '@/api/hooks'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'

export default function UnknownPeerAlert() {
  const { data: peers } = usePeers()
  const navigate = useNavigate()
  const [dismissedCount, setDismissedCount] = useState(0)

  const unknownPeers = (peers || []).filter(p => p.auto_discovered)
  const count = unknownPeers.length

  // Show if there are unknown peers and count has increased since last dismiss
  if (count === 0 || count <= dismissedCount) return null

  return (
    <Alert variant="destructive" className="mb-4">
      <AlertDescription className="flex items-center justify-between">
        <span>
          <strong>{count} unknown peer{count > 1 ? 's' : ''} detected</strong> — review required for security.
        </span>
        <div className="flex gap-2 ml-4 shrink-0">
          <Button variant="outline" size="sm" onClick={() => navigate('/peers?filter=auto_discovered')}>
            Review
          </Button>
          <Button variant="ghost" size="sm" onClick={() => setDismissedCount(count)}>
            Dismiss
          </Button>
        </div>
      </AlertDescription>
    </Alert>
  )
}

