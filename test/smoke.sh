#!/bin/bash
# wg-sockd Smoke Test Script
# Usage: ./test/smoke.sh [socket_path]
# Requires: curl, jq, running wg-sockd agent with WireGuard

set -euo pipefail

SOCK="${1:-/var/run/wg-sockd/wg-sockd.sock}"
PASS=0
FAIL=0

green() { echo -e "\033[32m✓ $*\033[0m"; }
red()   { echo -e "\033[31m✗ $*\033[0m"; }

check() {
    local desc="$1"
    shift
    local out
    out=$("$@" 2>&1)
    if [ $? -eq 0 ]; then
        green "$desc"
        PASS=$((PASS + 1))
    else
        red "$desc"
        [ -n "$out" ] && echo "  Output: $out"
        FAIL=$((FAIL + 1))
    fi
}

check_eq() {
    local desc="$1" expected="$2" actual="$3"
    if [ "$expected" = "$actual" ]; then
        green "$desc (got: $actual)"
        PASS=$((PASS + 1))
    else
        red "$desc (expected: $expected, got: $actual)"
        FAIL=$((FAIL + 1))
    fi
}

api() {
    curl -sf --unix-socket "$SOCK" "http://localhost$1" "${@:2}"
}

# Preflight checks
echo "=== wg-sockd Smoke Tests ==="
echo "Socket: $SOCK"
echo ""

if ! command -v jq &>/dev/null; then
    red "jq is required but not installed"
    exit 1
fi

if ! command -v curl &>/dev/null; then
    red "curl is required but not installed"
    exit 1
fi

if [ ! -S "$SOCK" ]; then
    red "Socket not found: $SOCK — is wg-sockd running?"
    exit 1
fi

# --- Test 1: Health ---
echo "--- Health ---"
HEALTH=$(api /api/health)
STATUS=$(echo "$HEALTH" | jq -r '.status')
check_eq "Health status" "ok" "$STATUS"

WG_STATUS=$(echo "$HEALTH" | jq -r '.wireguard')
check_eq "WireGuard status" "ok" "$WG_STATUS"

DB_STATUS=$(echo "$HEALTH" | jq -r '.sqlite')
check_eq "SQLite status" "ok" "$DB_STATUS"

# --- Test 2: Create Peer ---
echo ""
echo "--- Create Peer ---"
CREATE_RESP=$(api /api/peers -X POST -H "Content-Type: application/json" \
    -d '{"friendly_name":"smoke-test-peer","allowed_ips":["10.99.0.2/32"]}')

PEER_ID=$(echo "$CREATE_RESP" | jq -r '.id')
PEER_PUB=$(echo "$CREATE_RESP" | jq -r '.public_key')
PEER_PRIV=$(echo "$CREATE_RESP" | jq -r '.private_key')

check "Create peer returns ID" [ "$PEER_ID" != "null" ] && [ -n "$PEER_ID" ]
check "Create peer returns public_key" [ "$PEER_PUB" != "null" ] && [ -n "$PEER_PUB" ]
check "Create peer returns private_key" [ "$PEER_PRIV" != "null" ] && [ -n "$PEER_PRIV" ]

PEER_NAME=$(echo "$CREATE_RESP" | jq -r '.friendly_name')
check_eq "Peer friendly_name" "smoke-test-peer" "$PEER_NAME"

# --- Test 3: List Peers ---
echo ""
echo "--- List Peers ---"
LIST_RESP=$(api /api/peers)
PEER_COUNT=$(echo "$LIST_RESP" | jq 'length')
check "List peers returns at least 1" [ "$PEER_COUNT" -ge 1 ]

FOUND=$(echo "$LIST_RESP" | jq -r ".[] | select(.id == $PEER_ID) | .friendly_name")
check_eq "Created peer found in list" "smoke-test-peer" "$FOUND"

# --- Test 4: Download Conf ---
echo ""
echo "--- Download Conf ---"
CONF=$(api "/api/peers/$PEER_ID/conf")
check "Conf contains [Interface]" echo "$CONF" | grep -q "\[Interface\]"
check "Conf contains [Peer]" echo "$CONF" | grep -q "\[Peer\]"
check "Conf contains PersistentKeepalive" echo "$CONF" | grep -q "PersistentKeepalive"

# --- Test 5: Delete Peer ---
echo ""
echo "--- Delete Peer ---"
HTTP_CODE=$(curl -s -o /dev/null -w "%{http_code}" --unix-socket "$SOCK" \
    -X DELETE "http://localhost/api/peers/$PEER_ID")
check_eq "Delete returns 204" "204" "$HTTP_CODE"

# --- Test 6: Verify Deletion ---
echo ""
echo "--- Verify Deletion ---"
LIST_AFTER=$(api /api/peers)
FOUND_AFTER=$(echo "$LIST_AFTER" | jq -r ".[] | select(.id == $PEER_ID) | .id")
check_eq "Peer no longer in list" "" "$FOUND_AFTER"

# --- Test 7: IPv6 Peer (F7) ---
echo ""
echo "--- IPv6 Peer (F7) ---"
IPV6_RESP=$(api /api/peers -X POST -H "Content-Type: application/json" \
    -d '{"friendly_name":"smoke-ipv6","allowed_ips":["fd00::2/128"]}')

IPV6_ID=$(echo "$IPV6_RESP" | jq -r '.id')
check "IPv6 peer created" [ "$IPV6_ID" != "null" ] && [ -n "$IPV6_ID" ]

IPV6_IPS=$(echo "$IPV6_RESP" | jq -r '.allowed_ips[0]')
check_eq "IPv6 allowed_ips" "fd00::2/128" "$IPV6_IPS"

# Cleanup IPv6 peer
curl -sf --unix-socket "$SOCK" -X DELETE "http://localhost/api/peers/$IPV6_ID" >/dev/null 2>&1 || true

# --- Summary ---
echo ""
echo "==========================="
TOTAL=$((PASS + FAIL))
echo "Results: $PASS/$TOTAL passed"
if [ "$FAIL" -gt 0 ]; then
    red "$FAIL tests failed"
    exit 1
else
    green "All tests passed!"
    exit 0
fi
