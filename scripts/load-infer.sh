#!/usr/bin/env bash
# Load /infer with github.com/rakyll/hey (install: go install github.com/rakyll/hey@latest)
set -euo pipefail

BASE_URL="${BASE_URL:-http://127.0.0.1:8080}"
DURATION="${DURATION:-30s}"
CONCURRENCY="${CONCURRENCY:-50}"
# Set QPS=200 to cap request rate; empty or 0 = unlimited (max throughput)
QPS="${QPS:-}"

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PAYLOAD="${PAYLOAD:-${SCRIPT_DIR}/loadtest-payload.json}"

# Prefer PATH; else go install default locations (GOBIN, then GOPATH/bin).
resolve_hey() {
	if command -v hey >/dev/null 2>&1; then
		command -v hey
		return 0
	fi
	local gobin gopath_bin
	gobin="$(go env GOBIN 2>/dev/null || true)"
	if [[ -n "${gobin}" && -x "${gobin}/hey" ]]; then
		echo "${gobin}/hey"
		return 0
	fi
	gopath_bin="$(go env GOPATH 2>/dev/null || true)/bin"
	if [[ -x "${gopath_bin}/hey" ]]; then
		echo "${gopath_bin}/hey"
		return 0
	fi
	return 1
}

if ! HEY="$(resolve_hey)"; then
	echo "hey not found. Install with:"
	echo "  go install github.com/rakyll/hey@latest"
	echo "Then either add Go's bin dir to PATH, e.g.:"
	echo "  export PATH=\"\$(go env GOPATH)/bin:\$PATH\""
	[[ -n "$(go env GOBIN 2>/dev/null)" ]] && echo "  # or: export PATH=\"\$(go env GOBIN):\$PATH\""
	exit 1
fi

if [[ ! -f "${PAYLOAD}" ]]; then
	echo "Payload file not found: ${PAYLOAD}"
	exit 1
fi

# Fail fast if nothing is listening — otherwise hey reports bogus RPS / NaN and macOS may hit
# "no buffer space available" from millions of instant connection refused errors.
if [[ "${SKIP_PREFLIGHT:-}" != "1" ]]; then
	HEALTH_URL="${BASE_URL%/}/health"
	deadline=$((SECONDS + ${WAIT_FOR_SERVER:-0}))
	while true; do
		if curl -sf --connect-timeout 2 --max-time 3 "${HEALTH_URL}" >/dev/null 2>&1; then
			break
		fi
		if [[ "${WAIT_FOR_SERVER:-0}" =~ ^[0-9]+$ ]] && [[ "${WAIT_FOR_SERVER:-0}" -gt 0 ]] && [[ "${SECONDS}" -lt "${deadline}" ]]; then
			sleep 1
			continue
		fi
		echo "InferCore is not reachable at ${BASE_URL} (GET ${HEALTH_URL} failed)."
		echo "Start the server first, e.g.:"
		echo "  INFERCORE_CONFIG=configs/infercore.loadtest.yaml make run"
		echo "Wrong port?  BASE_URL=http://127.0.0.1:YOUR_PORT bash ./scripts/load-infer.sh"
		echo "Wait for startup:  WAIT_FOR_SERVER=30 bash ./scripts/load-infer.sh"
		echo "Skip this check:   SKIP_PREFLIGHT=1 bash ./scripts/load-infer.sh"
		exit 1
	done
fi

INFER_URL="${BASE_URL%/}/infer"
echo "InferCore load test"
echo "  hey:          ${HEY}"
echo "  URL:          ${INFER_URL}"
echo "  duration:     ${DURATION}"
echo "  concurrency:  ${CONCURRENCY}"
if [[ -n "${QPS}" && "${QPS}" != "0" ]]; then
	echo "  QPS cap:      ${QPS}"
fi
if [[ -n "${INFERCORE_API_KEY:-}" ]]; then
	echo "  API key:      (INFERCORE_API_KEY set)"
fi
echo

HEY_ARGS=(
	-z "${DURATION}"
	-c "${CONCURRENCY}"
	-m POST
	-T "application/json"
	-d @"${PAYLOAD}"
)
if [[ -n "${QPS}" && "${QPS}" != "0" ]]; then
	HEY_ARGS+=(-q "${QPS}")
fi
if [[ -n "${INFERCORE_API_KEY:-}" ]]; then
	HEY_ARGS+=(-H "X-InferCore-Api-Key: ${INFERCORE_API_KEY}")
fi

"${HEY}" "${HEY_ARGS[@]}" "${INFER_URL}"

echo
echo "=== Sample /metrics (infercore_*) ==="
if [[ -n "${INFERCORE_API_KEY:-}" ]]; then
	curl -sS -H "X-InferCore-Api-Key: ${INFERCORE_API_KEY}" "${BASE_URL%/}/metrics" | grep -E '^infercore_' | head -30 || true
else
	curl -sS "${BASE_URL%/}/metrics" | grep -E '^infercore_' | head -30 || true
fi
