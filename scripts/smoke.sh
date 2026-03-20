#!/usr/bin/env bash
set -euo pipefail

BASE_URL="${1:-http://localhost:8080}"

echo "Running InferCore smoke checks against: ${BASE_URL}"

echo "[1/4] /health"
HEALTH="$(curl -sS "${BASE_URL}/health")"
echo "${HEALTH}" | grep -q '"status":"ok"' || {
  echo "Health check failed: ${HEALTH}"
  exit 1
}

echo "[2/4] /status"
STATUS="$(curl -sS "${BASE_URL}/status")"
echo "${STATUS}" | grep -q '"service":"infercore"' || {
  echo "Status check failed: ${STATUS}"
  exit 1
}

echo "[3/4] /infer"
INFER="$(curl -sS -X POST "${BASE_URL}/infer" \
  -H "Content-Type: application/json" \
  -d '{
    "tenant_id":"team-a",
    "task_type":"simple",
    "input":{"text":"smoke test"},
    "options":{"stream":false,"max_tokens":64}
  }')"
echo "${INFER}" | grep -q '"status":"success"' || {
  echo "Infer check failed: ${INFER}"
  exit 1
}
echo "${INFER}" | grep -q '"trace_id"' || {
  echo "Infer response missing trace_id: ${INFER}"
  exit 1
}

echo "[4/4] /metrics"
METRICS="$(curl -sS "${BASE_URL}/metrics")"
echo "${METRICS}" | grep -q 'infercore_http_requests_total' || {
  echo "Metrics check failed"
  exit 1
}

echo "Smoke checks passed."
