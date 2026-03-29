# Nginx Docker DNS Dynamic Resolution Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make nginx dynamically re-resolve `go-api` in Docker so gateway keeps working after backend container recreation.

**Architecture:** Update nginx deployment config to use Docker DNS plus `resolve`, then lock the behavior in the existing compose/nginx contract test. Verify with focused tests and a compose smoke that rebuilds services and checks gateway health.

**Tech Stack:** nginx, Docker Compose, Go tests

---

### Task 1: Add failing contract test expectations

**Files:**
- Modify: `internal/config/compose_contract_test.go`
- Test: `internal/config/compose_contract_test.go`

- [ ] Step 1: Write the failing test expectation for Docker DNS resolver and dynamic upstream resolution.
- [ ] Step 2: Run `go test ./internal/config -count=1` and confirm it fails for the expected missing nginx config.
- [ ] Step 3: Update nginx config with `resolver 127.0.0.11 valid=10s ipv6=off;` and `server go-api:5000 resolve;`.
- [ ] Step 4: Re-run `go test ./internal/config -count=1` and confirm it passes.

### Task 2: Verify compose behavior

**Files:**
- Modify: `README.md` (if needed for deployment note)
- Verify: `deploy/nginx/canary-go-full.conf`, `docker-compose.yml`

- [ ] Step 1: Run focused verification for gateway health after the config change.
- [ ] Step 2: Run a compose smoke that rebuilds services and confirms `http://127.0.0.1:5002/healthz` returns 200.
- [ ] Step 3: Report the evidence from the commands.
