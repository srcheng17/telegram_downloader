#!/usr/bin/env bash
set -euo pipefail

# Uses only a self-owned loopback Docker instance and synthetic media. The test
# removes its container, credentials and temporary library even after failure.
# Pull gotson/komga:1.28.1 beforehand if it is not already cached.
cd "$(dirname "$0")/.."
KOMGA_DELIVERY_INTEGRATION=1 go test ./internal/app/komgaedit \
  -run '^TestDeliveryRealKomga1281$' -count=1 -v -timeout=5m
