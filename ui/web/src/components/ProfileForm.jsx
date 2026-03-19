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
  client_allowed_ips: '', use_preshared_key: false,
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
            placeholder="e.g. full-tunnel" disabled={!isNew} required /></div>
        <div>
          <FieldLabel>Description</FieldLabel>
          <Input value={formData.description} onChange={e => setFormData({ ...formData, description: e.target.value })}
            placeholder="e.g. Routes all traffic through VPN" /></div>
        <div className="flex items-center gap-2">
          <input id="usePresharedKey" type="checkbox"
            checked={formData.use_preshared_key}
            onChange={e => setFormData({ ...formData, use_preshared_key: e.target.checked })}
            className="h-4 w-4 rounded border-input" />
          <FieldLabel hint="When enabled, a random PresharedKey is automatically generated for every new peer created with this profile. The PSK is included in both the server wg.conf [Peer] section and the client download .conf [Peer] section. Adds post-quantum resistance to the tunnel.">
            Use PresharedKey
          </FieldLabel>
        </div>
      </fieldset>

      {/* ── Server config ── */}
      <fieldset className="space-y-3">
        <legend className="text-xs font-medium uppercase tracking-wider text-muted-foreground">Server config (wg.conf)</legend>
        <div>
          <FieldLabel hint="These CIDRs are written into the server's wg.conf file as [Peer] AllowedIPs for every peer in this profile. They control which traffic the server will accept from the peer and route to it. For example, 0.0.0.0/0 means the server routes all traffic to this peer (full-tunnel), while 10.0.0.0/8 limits routing to internal networks only.">
            Allowed IPs *
          </FieldLabel>
          <Input value={formData.allowed_ips} onChange={e => setFormData({ ...formData, allowed_ips: e.target.value })}
            placeholder="0.0.0.0/0" required /></div>
        <div>
          <FieldLabel hint="CIDRs listed here are subtracted from the Allowed IPs above before writing to the server's wg.conf. Useful to carve out exceptions — for example, allow 0.0.0.0/0 but exclude 10.0.0.0/8 and 172.16.0.0/12 to prevent routing private ranges through this peer.">
            Exclude IPs
          </FieldLabel>
          <Input value={formData.exclude_ips} onChange={e => setFormData({ ...formData, exclude_ips: e.target.value })}
            placeholder="10.0.0.0/8, 172.16.0.0/12" /></div>
        <div>
          <FieldLabel hint="Written into the server's wg.conf as [Peer] PersistentKeepalive. The server sends a keepalive packet every N seconds to keep the tunnel alive through NAT. Typical value: 25. Set to 0 to disable. Leave empty to inherit from the global config.yaml setting.">
            Default PersistentKeepalive
          </FieldLabel>
          <Input type="number" min="0" max="65535" value={formData.persistent_keepalive} onChange={e => setFormData({ ...formData, persistent_keepalive: e.target.value })}
            placeholder="seconds" /></div>
      </fieldset>

      {/* ── Client download config ── */}
      <fieldset className="space-y-3">
        <legend className="text-xs font-medium uppercase tracking-wider text-muted-foreground">Client download config (.conf)</legend>
        <div>
          <FieldLabel hint="Written into the client's download .conf file as [Interface] DNS. These are the DNS servers the client device will use while the VPN tunnel is active. Common choices: 1.1.1.1 (Cloudflare), 8.8.8.8 (Google), or your internal DNS server address. Leave empty to inherit from the global config.yaml setting.">
            Default Client DNS
          </FieldLabel>
          <Input value={formData.client_dns} onChange={e => setFormData({ ...formData, client_dns: e.target.value })}
            placeholder="1.1.1.1, 8.8.8.8" /></div>
        <div>
          <FieldLabel hint="Written into the client's download .conf file as [Interface] MTU. Controls the maximum packet size for the tunnel interface on the client device. Typical values: 1420 for most setups, 1280 for double-encapsulated tunnels. Leave empty to inherit from the global config.yaml setting or let WireGuard auto-detect.">
            Default Client MTU
          </FieldLabel>
          <Input type="number" min="0" max="9000" value={formData.client_mtu} onChange={e => setFormData({ ...formData, client_mtu: e.target.value })}
            placeholder="bytes" /></div>
        <div>
          <FieldLabel hint="Written into the client's download .conf file as [Peer] AllowedIPs. Controls which traffic the client device sends through the VPN tunnel. For example, 10.0.0.0/8 means only traffic to internal networks goes through VPN (split-tunnel), while everything else goes directly to the internet. Leave empty for full-tunnel mode where all client traffic goes through VPN (0.0.0.0/0, ::/0).">
            Client AllowedIPs
          </FieldLabel>
          <Input value={formData.client_allowed_ips} onChange={e => setFormData({ ...formData, client_allowed_ips: e.target.value })}
            placeholder="empty = full-tunnel (0.0.0.0/0, ::/0)" /></div>
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
