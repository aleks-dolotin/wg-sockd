import { useNavigate } from 'react-router-dom'
import { useProfiles } from '@/api/hooks'
import { usePageTitle } from '@/hooks/usePageTitle'
import { Button } from '@/components/ui/button'
import { Badge } from '@/components/ui/badge'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Skeleton } from '@/components/ui/skeleton'

function ProfilesSkeleton() {
  return (
    <div className="space-y-4">
      <div className="flex items-center justify-between">
        <Skeleton className="h-8 w-32" />
        <Skeleton className="h-9 w-32" />
      </div>
      {[...Array(3)].map((_, i) => (
        <Card key={i}><CardContent className="pt-6 space-y-2">
          <Skeleton className="h-5 w-40" />
          <Skeleton className="h-4 w-64" />
        </CardContent></Card>
      ))}
    </div>
  )
}

export default function ProfilesPage() {
  usePageTitle('Profiles')
  const { data: profiles, isLoading, error } = useProfiles()
  const navigate = useNavigate()

  if (isLoading) return <ProfilesSkeleton />
  if (error) return <p className="text-destructive">Error: {error.message}</p>

  return (
    <div className="space-y-4">
      <div className="flex items-center justify-between">
        <h2 className="text-2xl font-semibold tracking-tight">Profiles</h2>
        <Button onClick={() => navigate('/settings/profiles/new')}>Create Profile</Button>
      </div>

      <div className="grid gap-3">
        {(profiles || []).map(p => (
          <Card key={p.name} className="cursor-pointer hover:ring-1 hover:ring-foreground/20 transition-shadow"
            onClick={() => navigate(`/settings/profiles/${p.name}`)}>
            <CardHeader className="pb-2">
              <div className="flex items-center justify-between">
                <CardTitle className="text-base">
                  {p.name}
                  {p.is_default && <Badge variant="secondary" className="ml-2">Default</Badge>}
                </CardTitle>
              </div>
            </CardHeader>
            <CardContent className="text-sm text-muted-foreground space-y-1">
              {p.description && <p>{p.description}</p>}
              <p>{p.resolved_allowed_ips?.length || 0} resolved routes | {p.peer_count || 0} peers</p>
            </CardContent>
          </Card>
        ))}
        {(profiles || []).length === 0 && (
          <p className="text-muted-foreground text-sm">No profiles yet. Create one to get started.</p>
        )}
      </div>
    </div>
  )
}
