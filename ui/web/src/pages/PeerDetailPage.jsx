import { useParams } from 'react-router-dom'
import { usePeer } from '@/api/hooks'

export default function PeerDetailPage() {
  const { id } = useParams()
  const { data: peer, isLoading, error } = usePeer(id)

  if (isLoading) return <p className="text-gray-500">Loading peer…</p>
  if (error) return <p className="text-red-500">Error: {error.message}</p>

  return (
    <div className="space-y-4">
      <h2 className="text-2xl font-semibold tracking-tight">
        {peer?.friendly_name || `Peer ${id}`}
      </h2>
      <p className="text-gray-500">Peer detail view — expanded in Story 3-3.</p>
    </div>
  )
}

