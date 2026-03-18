#!/usr/bin/env bash
set -euo pipefail

if [ -z "${ANTHROPIC_API_KEY:-}" ]; then
  echo "ERROR: ANTHROPIC_API_KEY is not set" >&2
  exit 1
fi

TIMESTAMP=$(date -u +"%Y%m%dT%H%M%SZ")
OUTPUT_DIR="$(dirname "$0")/results/${TIMESTAMP}"
mkdir -p "${OUTPUT_DIR}"
OUTPUT_FILE="${OUTPUT_DIR}/output.txt"

echo "Running evals — output: ${OUTPUT_FILE}"
go test -v -tags=evals -timeout=120s ./evals/... 2>&1 | tee "${OUTPUT_FILE}"
echo "Done. Results saved to ${OUTPUT_FILE}"
