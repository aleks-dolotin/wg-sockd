#!/usr/bin/env bash
# Helm chart template validation tests for wg-sockd-ui
# Usage: bash chart/tests/validate.sh
set -euo pipefail

CHART_DIR="$(cd "$(dirname "$0")/.." && pwd)"
PASS=0
FAIL=0

pass() { ((PASS++)); echo "  ✅ $1"; }
fail() { ((FAIL++)); echo "  ❌ $1"; }

echo "=== wg-sockd-ui Helm Chart Validation ==="
echo ""

# --- Test 1: Chart lints successfully ---
echo "Test 1: helm lint"
if helm lint "$CHART_DIR" --quiet 2>/dev/null; then
  pass "Chart lints successfully"
else
  fail "Chart lint failed"
fi

# --- Test 2: Default values render ---
echo "Test 2: Default template rendering"
OUTPUT=$(helm template test-release "$CHART_DIR" 2>&1)
if [ $? -eq 0 ]; then
  pass "Template renders with default values"
else
  fail "Template rendering failed"
fi

# --- Test 3: Deployment has single replica (AC #1) ---
echo "Test 3: Single replica (AC #1)"
REPLICAS=$(echo "$OUTPUT" | grep "replicas:" | head -1 | awk '{print $2}')
if [ "$REPLICAS" = "1" ]; then
  pass "Deployment has 1 replica"
else
  fail "Expected 1 replica, got: $REPLICAS"
fi

# --- Test 4: hostPath DirectoryOrCreate (AC #1) ---
echo "Test 4: hostPath DirectoryOrCreate (AC #1)"
if echo "$OUTPUT" | grep -q "type: DirectoryOrCreate"; then
  pass "hostPath type is DirectoryOrCreate"
else
  fail "Missing DirectoryOrCreate hostPath type"
fi

# --- Test 5: hostPath mounts /var/run/wg-sockd/ (AC #1) ---
echo "Test 5: hostPath path (AC #1)"
if echo "$OUTPUT" | grep -q "path: /var/run/wg-sockd/"; then
  pass "hostPath path is /var/run/wg-sockd/"
else
  fail "Unexpected hostPath path"
fi

# --- Test 6: Default nodeSelector wg-sockd: active (AC #2) ---
echo "Test 6: Default nodeSelector (AC #2)"
if echo "$OUTPUT" | grep -q "wg-sockd: active"; then
  pass "nodeSelector: wg-sockd: active present"
else
  fail "Missing default nodeSelector"
fi

# --- Test 7: nodeName override removes nodeSelector (AC #2) ---
echo "Test 7: nodeName override (AC #2)"
NODE_OUTPUT=$(helm template test-release "$CHART_DIR" --set nodeName=my-node 2>&1)
if echo "$NODE_OUTPUT" | grep -q "nodeName: my-node"; then
  if ! echo "$NODE_OUTPUT" | grep -q "nodeSelector"; then
    pass "nodeName overrides nodeSelector"
  else
    fail "nodeSelector still present when nodeName is set"
  fi
else
  fail "nodeName not rendered"
fi

# --- Test 8: runAsNonRoot (AC #3) ---
echo "Test 8: runAsNonRoot (AC #3)"
if echo "$OUTPUT" | grep -q "runAsNonRoot: true"; then
  pass "runAsNonRoot: true"
else
  fail "Missing runAsNonRoot"
fi

# --- Test 9: runAsGroup 5000 (AC #3) ---
echo "Test 9: runAsGroup 5000 (AC #3)"
if echo "$OUTPUT" | grep -q "runAsGroup: 5000"; then
  pass "runAsGroup: 5000"
else
  fail "Missing runAsGroup: 5000"
fi

# --- Test 10: supplementalGroups 5000 (AC #3) ---
echo "Test 10: supplementalGroups (AC #3)"
if echo "$OUTPUT" | grep -A1 "supplementalGroups" | grep -q "5000"; then
  pass "supplementalGroups includes 5000"
else
  fail "Missing supplementalGroups 5000"
fi

# --- Test 11: readOnlyRootFilesystem (AC #3) ---
echo "Test 11: readOnlyRootFilesystem (AC #3)"
if echo "$OUTPUT" | grep -q "readOnlyRootFilesystem: true"; then
  pass "readOnlyRootFilesystem: true"
else
  fail "Missing readOnlyRootFilesystem"
fi

# --- Test 12: Service ClusterIP port 8080 (AC #4) ---
echo "Test 12: Service ClusterIP 8080 (AC #4)"
if echo "$OUTPUT" | grep -q "type: ClusterIP" && echo "$OUTPUT" | grep -q "port: 8080"; then
  pass "Service: ClusterIP on port 8080"
else
  fail "Service not ClusterIP:8080"
fi

# --- Test 13: Container args pass socket path ---
echo "Test 13: Container socket arg"
if echo "$OUTPUT" | grep -q "/var/run/wg-sockd/wg-sockd.sock"; then
  pass "Container receives socket path via args"
else
  fail "Container missing socket path arg"
fi

# --- Test 14: Readiness and liveness probes ---
echo "Test 14: Health probes"
if echo "$OUTPUT" | grep -q "readinessProbe" && echo "$OUTPUT" | grep -q "livenessProbe"; then
  pass "Readiness and liveness probes configured"
else
  fail "Missing health probes"
fi

# --- Summary ---
echo ""
echo "=== Results: $PASS passed, $FAIL failed ==="
if [ "$FAIL" -gt 0 ]; then
  exit 1
fi

