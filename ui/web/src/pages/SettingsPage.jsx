import { useState } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { toast } from 'sonner'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Card, CardContent, CardHeader, CardTitle, CardDescription } from '@/components/ui/card'
import { Dialog, DialogContent, DialogHeader, DialogTitle, DialogFooter, DialogDescription } from '@/components/ui/dialog'
import {
  webauthnListCredentials,
  webauthnRegisterBegin,
  webauthnRegisterFinish,
  webauthnDeleteCredential,
} from '@/api/client'
import { prepareCreationOptions, serializeCreationResponse } from '@/lib/webauthn'

/** Format ISO date string as relative time (e.g. "3 days ago") */
function relativeTime(isoString) {
  if (!isoString) return 'Never'
  const diff = Date.now() - new Date(isoString).getTime()
  const mins = Math.floor(diff / 60_000)
  if (mins < 1) return 'Just now'
  if (mins < 60) return `${mins}m ago`
  const hrs = Math.floor(mins / 60)
  if (hrs < 24) return `${hrs}h ago`
  const days = Math.floor(hrs / 24)
  if (days < 30) return `${days}d ago`
  const months = Math.floor(days / 30)
  return `${months}mo ago`
}

export default function SettingsPage() {
  const queryClient = useQueryClient()

  // webauthn_enabled comes from session cache (already fetched by useAuth)
  const sessionData = queryClient.getQueryData(['auth', 'session'])
  const webauthnEnabled = sessionData?.webauthn_enabled ?? false

  const { data: credentials = [], isLoading } = useQuery({
    queryKey: ['webauthn', 'credentials'],
    queryFn: webauthnListCredentials,
    enabled: webauthnEnabled,
  })

  // Add passkey state
  const [adding, setAdding] = useState(false)
  const [friendlyName, setFriendlyName] = useState('')

  // Delete confirmation state
  const [deleteTarget, setDeleteTarget] = useState(null) // { id, friendly_name }
  const [deleting, setDeleting] = useState(false)

  // ---------------------------------------------------------------------------
  // Add passkey flow
  // ---------------------------------------------------------------------------
  async function handleAddPasskey() {
    setAdding(true)
    try {
      const beginResp = await webauthnRegisterBegin(friendlyName)
      const publicKeyOptions = prepareCreationOptions(beginResp.publicKey)

      const credential = await navigator.credentials.create({ publicKey: publicKeyOptions })
      if (!credential) return

      await webauthnRegisterFinish({
        credential: serializeCreationResponse(credential),
        token: beginResp.token,
        friendly_name: friendlyName,
      })

      toast.success('Passkey registered successfully')
      setFriendlyName('')
      queryClient.invalidateQueries({ queryKey: ['webauthn', 'credentials'] })
      queryClient.invalidateQueries({ queryKey: ['auth', 'session'] })
    } catch (err) {
      if (err?.name === 'NotAllowedError') {
        toast.info('Registration cancelled')
        return
      }
      if (err?.status === 401) {
        toast.error('Session expired. Please log in again.')
        return
      }
      if (err?.status === 409) {
        toast.error('This credential is already registered.')
        return
      }
      toast.error('Registration failed. Please try again.')
      console.error('[WebAuthn] register error:', err)
    } finally {
      setAdding(false)
    }
  }

  // ---------------------------------------------------------------------------
  // Delete passkey flow
  // ---------------------------------------------------------------------------
  async function handleDeleteConfirm() {
    if (!deleteTarget) return
    setDeleting(true)
    try {
      await webauthnDeleteCredential(deleteTarget.id)
      toast.success(`Passkey "${deleteTarget.friendly_name}" removed`)
      setDeleteTarget(null)
      queryClient.invalidateQueries({ queryKey: ['webauthn', 'credentials'] })
      queryClient.invalidateQueries({ queryKey: ['auth', 'session'] })
    } catch (err) {
      toast.error('Failed to delete passkey. Please try again.')
      console.error('[WebAuthn] delete error:', err)
    } finally {
      setDeleting(false)
    }
  }

  const isLastPasskey = credentials.length === 1

  // ---------------------------------------------------------------------------
  // Render
  // ---------------------------------------------------------------------------
  return (
    <div className="space-y-6 max-w-2xl">
      <div>
        <h2 className="text-2xl font-bold tracking-tight">Settings</h2>
        <p className="text-sm text-muted-foreground mt-1">Manage authentication settings.</p>
      </div>

      <Card>
        <CardHeader>
          <CardTitle>Passkeys</CardTitle>
          <CardDescription>
            Passkeys let you sign in with Touch ID, Face ID, or a hardware security key — no password needed.
          </CardDescription>
        </CardHeader>
        <CardContent className="space-y-4">
          {!webauthnEnabled ? (
            <p className="text-sm text-muted-foreground">
              Enable <code className="text-xs bg-muted px-1 py-0.5 rounded">auth.webauthn.enabled</code> in server config to manage passkeys.
            </p>
          ) : (
            <>
              {/* Credentials table */}
              {isLoading ? (
                <p className="text-sm text-muted-foreground">Loading…</p>
              ) : credentials.length === 0 ? (
                <p className="text-sm text-muted-foreground">
                  No passkeys registered. Add one to enable passwordless login.
                </p>
              ) : (
                <div className="border rounded-md overflow-hidden">
                  <table className="w-full text-sm">
                    <thead>
                      <tr className="border-b bg-muted/50">
                        <th className="text-left px-4 py-2 font-medium">Name</th>
                        <th className="text-left px-4 py-2 font-medium">Created</th>
                        <th className="text-left px-4 py-2 font-medium">Last used</th>
                        <th className="px-4 py-2" />
                      </tr>
                    </thead>
                    <tbody>
                      {credentials.map((cred) => (
                        <tr key={cred.id} className="border-b last:border-0 hover:bg-muted/30">
                          <td className="px-4 py-2 font-medium">
                            {cred.friendly_name || <span className="text-muted-foreground italic">Unnamed</span>}
                          </td>
                          <td className="px-4 py-2 text-muted-foreground">{relativeTime(cred.created_at)}</td>
                          <td className="px-4 py-2 text-muted-foreground">{relativeTime(cred.last_used_at)}</td>
                          <td className="px-4 py-2 text-right">
                            <Button
                              variant="ghost"
                              size="sm"
                              className="text-destructive hover:text-destructive"
                              onClick={() => setDeleteTarget(cred)}
                            >
                              Remove
                            </Button>
                          </td>
                        </tr>
                      ))}
                    </tbody>
                  </table>
                </div>
              )}

              {/* Add Passkey */}
              <div className="flex gap-2 items-end pt-2">
                <div className="flex-1 space-y-1">
                  <label className="text-xs text-muted-foreground">Name (optional)</label>
                  <Input
                    placeholder="e.g. MacBook Touch ID"
                    value={friendlyName}
                    onChange={(e) => setFriendlyName(e.target.value)}
                    maxLength={64}
                    disabled={adding}
                  />
                </div>
                <Button onClick={handleAddPasskey} disabled={adding}>
                  {adding ? (
                    <span className="flex items-center gap-2">
                      <span className="animate-spin rounded-full h-4 w-4 border-b-2 border-current" />
                      Registering…
                    </span>
                  ) : (
                    'Add Passkey'
                  )}
                </Button>
              </div>
            </>
          )}
        </CardContent>
      </Card>

      {/* Delete confirmation dialog */}
      <Dialog open={!!deleteTarget} onOpenChange={(open) => !open && setDeleteTarget(null)}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Remove passkey?</DialogTitle>
            <DialogDescription>
              {deleteTarget && (
                <>
                  Are you sure you want to remove <strong>{deleteTarget.friendly_name || 'this passkey'}</strong>?
                  {isLastPasskey && (
                    <span className="block mt-2 text-amber-600 font-medium">
                      ⚠️ This is your only passkey. You will need your password to sign in.
                    </span>
                  )}
                </>
              )}
            </DialogDescription>
          </DialogHeader>
          <DialogFooter>
            <Button variant="outline" onClick={() => setDeleteTarget(null)} disabled={deleting}>
              Cancel
            </Button>
            <Button variant="destructive" onClick={handleDeleteConfirm} disabled={deleting}>
              {deleting ? 'Removing…' : 'Remove'}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  )
}
