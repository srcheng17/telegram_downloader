# Nginx Docker DNS Dynamic Resolution Design

**Goal:** Make the gateway continue routing to `go-api` after `go-api` is rebuilt and gets a new container IP, without requiring a manual gateway restart.

**Problem:** The current nginx upstream resolves `go-api` once at startup. When `go-api` is recreated by `docker compose up -d --build`, the container IP can change while nginx continues sending traffic to the stale IP, producing `502 Bad Gateway` until the gateway container is restarted.

**Chosen approach:** Use Docker's embedded DNS server (`127.0.0.11`) in nginx and mark the upstream server with `resolve` so nginx periodically re-resolves `go-api`.

## Design

### Nginx behavior
- Add `resolver 127.0.0.11 valid=10s ipv6=off;` inside the server block.
- Change the upstream entry to `server go-api:5000 resolve;`.
- Keep existing route structure unchanged so only name-resolution behavior changes.

### Verification coverage
- Extend the existing compose/nginx contract test so it fails if:
  - the nginx config does not include Docker DNS resolver `127.0.0.11`
  - the `go-api:5000` upstream entry is missing `resolve`
- Preserve existing routing assertions for `/`, `/download`, `/api/*`, and `/healthz`.

### Risks / trade-offs
- This is nginx-specific config, but it is narrowly scoped to the Docker deployment already used in this repo.
- A short DNS cache window (`valid=10s`) means failover after rebuild is near-automatic while avoiding excessive lookups.
- This does not change application code or service topology.

### Acceptance criteria
- Gateway config contains Docker DNS resolver and dynamic upstream resolution.
- Contract tests cover both requirements.
- After rebuilding `go-api`, gateway health recovers without manually restarting `gateway`.
