# Profiles and Peer Configuration — WYSIWYG Model

## Design Principle

**What You See Is What You Get.** Every field value visible in the UI is exactly what goes into the generated .conf file. There are no hidden lookups, no cascaded inheritance, no magic.

## What is a Profile?

A profile is a **pre-fill template** for creating peers. When a user selects a profile during peer creation:

1. The form fields are populated with the profile's non-empty values
2. All fields remain fully editable — the user can override any value
3. On submit, only the values from the form are sent to the API
4. The backend stores exactly what the client sent

Profiles are **not** consulted at .conf generation time. Once a peer is created, it stores all config values explicitly. Changing a profile does not affect existing peers.

## Client .conf Generation

The client download .conf is generated directly from peer fields:

- If a peer has `client_dns: "9.9.9.9"`, the .conf contains `DNS = 9.9.9.9`
- If a peer has an empty `client_dns`, the .conf does **not** contain a `DNS =` line
- No profile, global default, or hardcoded fallback is consulted

## Required Fields

| Field | Why Required |
|-------|-------------|
| Friendly Name | Peer identification |
| Allowed IPs (or Profile) | Server wg.conf `[Peer] AllowedIPs` |
| Client Address | Client .conf `[Interface] Address` |
| Client AllowedIPs | Client .conf `[Peer] AllowedIPs` |

## Optional Fields

| Field | What Happens if Empty |
|-------|----------------------|
| DNS | No `DNS =` line — client uses system DNS |
| MTU | No `MTU =` line — WireGuard auto-detects |
| PersistentKeepalive | No `PersistentKeepalive =` line — no keepalive packets |
| PresharedKey | No `PresharedKey =` line — standard encryption only |
| Endpoint | No `Endpoint =` line in server wg.conf — client-initiated only |

## PresharedKey Behavior

PSK generation is controlled **entirely by the client request**:

- The API generates a PSK only when `preshared_key: "auto"` is explicitly sent
- The profile's `use_preshared_key` flag pre-checks the UI checkbox
- The backend does NOT check the profile's flag — it respects only what the client sends
- After creation, PSK can only be regenerated via the Rotate Keys action

## Profile Fields Reference

| Field | Description |
|-------|-------------|
| Name | Unique identifier |
| Description | Human-readable description |
| Use PresharedKey | Pre-checks "Generate PSK" checkbox for new peers |
| Allowed IPs | CIDRs for server wg.conf `[Peer] AllowedIPs` |
| Exclude IPs | Subtracted from Allowed IPs via CIDR calculator |
| PersistentKeepalive | Pre-fills form field |
| Client DNS | Pre-fills form field |
| Client MTU | Pre-fills form field |
| Client AllowedIPs | Pre-fills form field |

## Migration from Cascade Model

Prior to v0.14.0, wg-sockd used a 4-level cascade (peer → profile → global → hardcoded) to resolve client .conf values. This has been removed. See UPGRADING.md for migration instructions.
