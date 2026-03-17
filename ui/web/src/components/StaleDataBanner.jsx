import { useConnection } from '@/components/ConnectionContext'
import { Alert, AlertDescription } from '@/components/ui/alert'

export default function StaleDataBanner() {
  const { isConnected } = useConnection()

  if (isConnected) return null

  return (
    <div className="max-w-5xl mx-auto w-full px-4 pt-3">
      <Alert className="border-amber-500/50 bg-amber-50 text-amber-900 dark:bg-amber-950/30 dark:text-amber-200 dark:border-amber-500/30">
        <AlertDescription className="flex items-center gap-2">
          <span className="shrink-0">⚠️</span>
          <span>Agent unavailable — data may be outdated</span>
        </AlertDescription>
      </Alert>
    </div>
  )
}
