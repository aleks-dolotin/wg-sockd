import { useConnection } from './ConnectionContext'

const colors = {
  connected: 'bg-green-500',
  connecting: 'bg-yellow-500',
  disconnected: 'bg-red-500',
  unknown: 'bg-gray-400',
}

const labels = {
  connected: 'Connected',
  connecting: 'Connecting to agent...',
  disconnected: 'Agent unavailable',
  unknown: 'Unknown',
}

export default function ConnectionStatus() {
  const { state } = useConnection()

  return (
    <div className="flex items-center gap-1.5 text-xs">
      <span className={`w-2 h-2 rounded-full ${colors[state] || colors.unknown}`} />
      <span className="hidden sm:inline text-gray-600">{labels[state] || state}</span>
    </div>
  )
}

