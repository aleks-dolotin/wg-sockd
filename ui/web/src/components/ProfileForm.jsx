import { useState } from 'react'
import { TooltipProvider } from '@/components/ui/tooltip'
import FieldLabel from '@/components/FieldLabel'
import { Input } from '@/components/ui/input'
import { Button } from '@/components/ui/button'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { formDataToPayload } from '@/lib/profile-form-utils'

const emptyForm = {
  name: '', allowed_ips: '', exclude_ips: '', description: '',
  persistent_keepalive: '', client_dns: '', client_mtu: '',
  use_preshared_key: false,
}


export default function ProfileForm({ initialData, isNew = false, onSubmit, isPending, error, onCancel }) {
  const [formData, setFormData] = useState(initialData || emptyForm)

  function handleSubmit(e) {
    e.preventDefault()
    onSubmit(formDataToPayload(formData))
  }

  return (
    <form onSubmit={handleSubmit} className="space-y-6 max-w-lg">
      <TooltipProvider>
      {/* ── General ── */}
      <fieldset className="space-y-3">
        <legend className="text-xs font-medium uppercase tracking-wider text-muted-foreground">General</legend>
        <div>
          <FieldLabel>Name</FieldLabel>
          <Input value={formData.name} onChange={e => setFormData({ ...formData, name: e.target.value })}
            placeholder="e.g. internal-access" disabled={!isNew} required /></div>
        <div>
          <FieldLabel>Description</FieldLabel>
          <Input value={formData.description} onChange={e => setFormData({ ...formData, description: e.target.value })}
            placeholder="e.g. Access to internal 10.x networks" /></div>
        <div className="flex items-center gap-2">
          <input id="usePresharedKey" type="checkbox"
            checked={formData.use_preshared_key}
            onChange={e => setFormData({ ...formData, use_preshared_key: e.target.checked })}
            className="h-4 w-4 rounded border-input" />
          <FieldLabel hint="Auto-generate a PresharedKey for every new peer in this profile.">
            Use PresharedKey
          </FieldLabel>
        </div>
      </fieldset>

      {/* ── Network Access (client routing) ── */}
      <fieldset className="space-y-3">
        <legend className="text-xs font-medium uppercase tracking-wider text-muted-foreground">Network Access (client routing)</legend>
        <div>
          <FieldLabel hint="Networks peers can reach through VPN. After subtracting Exclude Networks, the result becomes the client's [Peer] AllowedIPs. Example: '10.0.0.0/8' for internal access, '0.0.0.0/0' for full-tunnel.">
            Allowed Networks *
          </FieldLabel>
          <Input value={formData.allowed_ips} onChange={e => setFormData({ ...formData, allowed_ips: e.target.value })}
            placeholder="0.0.0.0/0" required /></div>
        <div>
          <FieldLabel hint="Subtracted from Allowed Networks. Example: allow '0.0.0.0/0' but exclude '10.0.0.0/8' to route everything except internal traffic through VPN.">
            Exclude Networks
          </FieldLabel>
          <Input value={formData.exclude_ips} onChange={e => setFormData({ ...formData, exclude_ips: e.target.value })}
            placeholder="10.0.0.0/8, 172.16.0.0/12" /></div>
      </fieldset>

      {/* ── Server-side defaults ── */}
      <fieldset className="space-y-3">
        <legend className="text-xs font-medium uppercase tracking-wider text-muted-foreground">Server-side defaults</legend>
        <div>
          <FieldLabel hint="Default for peers in this profile. Can be overridden per-peer.">
            Default PersistentKeepalive
          </FieldLabel>
          <Input type="number" min="0" max="65535" value={formData.persistent_keepalive} onChange={e => setFormData({ ...formData, persistent_keepalive: e.target.value })}
            placeholder="seconds" /></div>
      </fieldset>

      {/* ── Client config defaults ── */}
      <fieldset className="space-y-3">
        <legend className="text-xs font-medium uppercase tracking-wider text-muted-foreground">Client config defaults</legend>
        <div>
          <FieldLabel hint="Default DNS for peers in this profile. Can be overridden per-peer.">
            Default DNS
          </FieldLabel>
          <Input value={formData.client_dns} onChange={e => setFormData({ ...formData, client_dns: e.target.value })}
            placeholder="1.1.1.1, 8.8.8.8" /></div>
        <div>
          <FieldLabel hint="Default MTU for peers in this profile. Can be overridden per-peer.">
            Default MTU
          </FieldLabel>
          <Input type="number" min="0" max="9000" value={formData.client_mtu} onChange={e => setFormData({ ...formData, client_mtu: e.target.value })}
            placeholder="bytes" /></div>
      </fieldset>
      </TooltipProvider>

      {error && <Alert variant="destructive"><AlertDescription>{error.message}</AlertDescription></Alert>}
      <div className="flex gap-2">
        <Button type="submit" disabled={isPending}>{isPending ? 'Saving...' : 'Save'}</Button>
        <Button type="button" variant="outline" onClick={onCancel}>Cancel</Button>
      </div>
    </form>
  )
}
