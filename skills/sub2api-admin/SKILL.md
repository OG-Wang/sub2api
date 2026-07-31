---
name: sub2api-admin
description: Manage Sub2API admin APIs for accounts, redeem codes, groups, proxies, error passthrough rules, TLS fingerprint profiles, imports, exports, batch updates, and raw administrator API calls. Use when the user mentions Sub2API, admin API keys, account management, redeem code management, recharge codes, invitation codes, bulk account import/export, keeping or deleting accounts, refreshing accounts, clearing errors, CRS sync, or managing Sub2API backend settings through the admin API.
---

# Sub2API Admin

Use the bundled CLI instead of ad hoc `curl`. Run examples from this skill directory.

```bash
export SUB2API_BASE_URL='https://your-sub2api-host'
export SUB2API_ADMIN_API_KEY='<admin api key>'
# Or, when the deployment uses admin JWT login instead of an admin API key:
# export SUB2API_JWT='<admin access_token>'
node scripts/sub2api-admin.js accounts list
```

For all commands and payload examples, read [references/admin-cli.md](references/admin-cli.md).

## This machine (ricktoken / research_zzz)

User-level Windows env vars are already set for this deployment. Prefer them; do **not** hardcode secrets into this skill.

| Variable | Purpose | Typical value |
|---|---|---|
| `SUB2API_BASE_URL` | Public admin/API base | `https://ricktoken.de5.net` |
| `SUB2API_ADMIN_API_KEY` | Admin `x-api-key` | User env (do not paste into chat/skill) |

### Auth discovery order

1. **Process env** in the current shell: `SUB2API_BASE_URL` + `SUB2API_ADMIN_API_KEY` (or `SUB2API_JWT`).
2. **Windows User/Machine env** if process env is empty (Claude Code / Git Bash often starts without newly set User vars):

```bash
# Git Bash: import User-scope vars into this shell (do not echo the key)
eval "$(powershell.exe -NoProfile -Command "
  \$base = [Environment]::GetEnvironmentVariable('SUB2API_BASE_URL','User')
  if (-not \$base) { \$base = [Environment]::GetEnvironmentVariable('SUB2API_BASE_URL','Machine') }
  \$key = [Environment]::GetEnvironmentVariable('SUB2API_ADMIN_API_KEY','User')
  if (-not \$key) { \$key = [Environment]::GetEnvironmentVariable('SUB2API_ADMIN_API_KEY','Machine') }
  if (\$base) { 'export SUB2API_BASE_URL=' + [char]39 + \$base + [char]39 }
  if (\$key)  { 'export SUB2API_ADMIN_API_KEY=' + [char]39 + \$key + [char]39 }
")"
```

3. **Only if env is truly missing**: recover from server DB `settings.admin_api_key` over SSH (`ubuntu@43.133.209.228`, deploy dir `~/sub2api-deploy`). Do not print the full key; only confirm prefix/length. Prefer asking the user to fix env over reading DB.

Notes for this deploy:

- Cloudflare orange-cloud may block non-browser UAs for some paths; admin CLI via `https://ricktoken.de5.net` with `x-api-key` is the normal path. Server-side `127.0.0.1:8080` is the fallback.
- `ADMIN_PASSWORD` in server `~/sub2api-deploy/.env` is empty after bootstrap; JWT password login is not the default here. Use admin API key.
- Never write the raw admin key into `SKILL.md`, memory, or chat unless the user explicitly asks.

## Workflow

1. Load `SUB2API_BASE_URL` + `SUB2API_ADMIN_API_KEY` (see discovery order above).
2. Run read-only commands first: `accounts list`, `accounts get <id>`, `groups all`, or `proxies all`.
3. Before destructive or bulk writes, print the target account names and IDs.
4. Execute the write command only after the target set is clear.
5. Run a follow-up read command to verify the result.

## Common Commands

```bash
node scripts/sub2api-admin.js accounts list --page-size 20
node scripts/sub2api-admin.js accounts get 40
node scripts/sub2api-admin.js accounts usage 40
node scripts/sub2api-admin.js accounts set-schedulable 40 true
node scripts/sub2api-admin.js accounts bulk-update --ids 40,39 --json '{"concurrency":10}'
node scripts/sub2api-admin.js redeem-codes list --page-size 20
node scripts/sub2api-admin.js redeem-codes generate --json '{"count":1,"type":"balance","value":10}' --idempotency-key redeem-$(date +%s)
node scripts/sub2api-admin.js redeem-codes create-and-redeem --json '{"code":"order_123","type":"balance","value":10,"user_id":123}' --idempotency-key order-123
node scripts/sub2api-admin.js error-rules list
node scripts/sub2api-admin.js tls-profiles list
```

## What this skill can / cannot do in one shot

Built-in building blocks:

- `accounts list --group grok --platform grok ...` — list/filter
- `accounts usage <id>` / `accounts today-stats` / `accounts batch-today-stats` — per-account usage
- `accounts set-schedulable <id> false` — single account **正常 → 暂停**
- `accounts bulk-update --ids ... --json '{"schedulable":false}'` — bulk pause/resume

**No** dedicated command like `pause-grok-24h-exhausted`. Tasks such as “把 grok 分组 24H 用量到 100% 的账号暂停” need a **compose**:

1. Identify targets (group + local 24h tokens ≥ limit, or usage snap 429).
2. Print IDs/names.
3. `bulk-update` with `{"schedulable":false}`.
4. Re-list / SQL verify.

Grok Free 24h bar in admin UI uses local `usage_logs` over rolling 24h against ~1_000_000 tokens (`xai.GrokFreeRolling24hTokenLimit`), not billing `usage_percent` (Free billing often has none).

## Safety Notes

- Authentication uses `x-api-key` from `SUB2API_ADMIN_API_KEY` first, then falls back to `Authorization: Bearer <jwt>` from `SUB2API_JWT`.
- If the API returns `INVALID_ADMIN_KEY`, ask the user to regenerate the admin API key. If using JWT, log in as an admin user and copy the `access_token` from `POST /api/v1/auth/login`.
- `accounts export` includes credentials and tokens. Prefer `--file` and avoid printing exports in chat.
- Redeem code create/redeem commands should use `--idempotency-key` for payment or recharge workflows.
- For uncertain or newly added backend APIs, use `api <METHOD> <admin-path>` after a read-only check.
