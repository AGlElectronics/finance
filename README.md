# Finance

Finance module for the Con Services stack — a proof of concept for
customers, outgoing invoices, payments, expenses, a running ledger and a
cash-versus-owed dashboard.
No bank connection yet; payments are recorded by hand (see
[docs/domain-model.md](docs/domain-model.md)).

- **Backend:** Go 1.27, chi, pgx (hand-written queries; no sqlc).
- **Frontend:** React 19 + Rsbuild + TanStack Router/Query + Tailwind.
- **Database:** PostgreSQL 16, migrated via Goose (`cmd/migrate` one-shot).
- **Object storage:** Finance stores **no files** in this phase, so no S3 server is
  intentionally wired into the standalone Compose stack. The `S3_*` env
  schema is still read by `config.Load` so the first file feature (invoice
  PDFs) can land without config churn. The suite's S3 server (SeaweedFS) is **local-dev only** and
  must never ship to production.

## Reserved ports

| Service         | Host port | Container port |
| --------------- | --------- | -------------- |
| backend (API)   | **8130**  | 8080           |
| frontend (SPA)  | **3006**  | 80             |
| postgres        | **5438**  | 5432           |
| Adminer (local) | 8083      | 8080           |

Every module in this ecosystem binds `:8080` *inside* its container; the
host-side port is reserved per-module (Finance = 8130) and the Compose file
maps `host:8130 -> container:8080`. The host ports avoid colliding with
stash (8088/3000), den (8090/3001), rally (8100/3002), hr (8110/3003/5436)
and learn (8120/3005/5437) on the same dev machine.

## Auth model

Same as the other modules:

- `AUTH_MODE=dev` (standalone default): the `X-Dev-User` header (default
  `dev-finance-admin`) stamps the actor; no gate.
- `AUTH_MODE=oidc` (suite): Bearer ID token from the shared Keycloak realm
  `conservices`, client `finance-spa`. Access requires membership of a group
  in `FINANCE_ACCESS_GROUPS` (default `finance-admin`).

Finance serves the public `GET /api/role-catalog` contract, so HR's access
picker can grant the `finance-admin` role during onboarding.

## First-time boot (standalone)

Requirements: Docker Desktop (or equivalent) with Compose v2, and no local
processes listening on the host ports above.

```sh
cd finance
docker compose -f infra/docker-compose.yml up --build
```

What happens:

1. `postgres` boots and reports healthy.
2. `migrate` (one-shot) applies all pending Goose migrations and exits 0.
3. `backend` starts on container `:8080`, published on host `:8130`. It
   does **not** auto-migrate.
4. `frontend` builds the SPA, serves it from nginx on `:3006`, and proxies
   `/api/...` + `/livez` + `/readyz` to the backend over `backend:8080`.

Health probes:

```sh
curl -s http://localhost:8130/livez   # from host → backend direct
curl -s http://localhost:3006/livez   # from host → frontend → backend
curl -s http://localhost:3006/api/me  # dev identity echo
```

To enable the local-only Adminer (`http://localhost:8083`, server
`postgres`, user/password/database `finance`):

```sh
docker compose \
  -f infra/docker-compose.yml \
  -f infra/docker-compose.local.yml up --build
```

## In the suite

The root `docker-compose.yml` of the parent repo wires Finance against the
shared Postgres, Keycloak (`finance-spa`) and Caddy
(`https://finance.conservices.test`). Pair it with Den from Finance →
Settings to follow the organisation's branding.

## Standalone image build

```sh
cd finance
docker build -f infra/Dockerfile.backend  -t finance-backend  .
docker build -f infra/Dockerfile.frontend -t finance-frontend .
docker build -f infra/Dockerfile.migrate  -t finance-migrate  .
```

All three Dockerfiles expect the build context to be `finance/` (not
`finance/backend` or `finance/frontend`) so they can see both the source
tree and `infra/nginx.conf`.

## Backups

In the full Con Services stack, Den's operator backs up Finance's database
every night along with everything else (see
[docs/operations.md](../docs/operations.md)).

**Run standalone, or on a database server Den's operator doesn't manage,
Finance is not backed up by anything. That is the administrator's job:**
schedule a `pg_dump --format=custom` of the finance database, encrypt it,
and keep it somewhere other than the server. Invoices are financial records
with legal retention periods — losing them is not an option.

## Working locally without Docker

```sh
# Backend
cd finance/backend
go mod tidy
DATABASE_URL='postgres://finance:finance@localhost:5438/finance?sslmode=disable' \
  go run ./cmd/migrate      # applies pending migrations
HTTP_ADDR=:8130 \
DATABASE_URL='postgres://finance:finance@localhost:5438/finance?sslmode=disable' \
  go run ./cmd/server       # pick ANY free host port; container default is :8080

# Frontend
cd finance/frontend
npm install
npm run dev                 # serves on :3006, proxies /api to :8130
```

## Repository layout

```
finance/
├── backend/
│   ├── cmd/
│   │   ├── migrate/      # one-shot Goose runner
│   │   └── server/       # API; NEVER migrates
│   ├── internal/         # app, config, auth, denlink, http, service, domain, …
│   └── migrations/       # embedded via migrations/embed.go
├── docs/                 # domain model
├── frontend/             # Rsbuild SPA
└── infra/                # Docker Compose stack + Dockerfiles + nginx
```
