<div align="center">

# 🎬 CineBook

**Concurrent cinema seat booking without the double-booking race.**

Hold a seat · Confirm it · Release it — Redis guarantees only one person wins.

[![CI](https://github.com/abdullahsamir74/cinema-booking/actions/workflows/ci.yml/badge.svg)](https://github.com/abdullahsamir74/cinema-booking/actions/workflows/ci.yml)
[![Go](https://img.shields.io/badge/Go-1.27-00ADD8?logo=go&logoColor=white)](https://go.dev)
[![Redis](https://img.shields.io/badge/Redis-8-DC382D?logo=redis&logoColor=white)](https://redis.io)
[![Docker](https://img.shields.io/badge/Docker-Compose-2496ED?logo=docker&logoColor=white)](https://docs.docker.com/compose/)
[![License](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)

[Quickstart](#-quickstart) · [API](#-api) · [How it works](#-how-it-works) · [OpenAPI](docs/openapi.yaml)

</div>

---

## ✨ What you get

| | |
| --- | --- |
| 🎟️ **Booking lifecycle** | Hold → confirm → release, with automatic expiry |
| ⚡ **Atomic seat locking** | Redis `SET NX` + Lua scripts — exactly one winner |
| 🧠 **Clean architecture** | Domain and use cases never import HTTP or Redis |
| 🔌 **Pluggable storage** | Redis for production, in-memory for local dev/tests |
| 🖥️ **Embedded UI** | Responsive frontend, no Node build step |
| 🧪 **Tested** | Race-detector tests, HTTP tests, real-Redis integration tests |
| 🐳 **One-command deploy** | Docker Compose with health checks and persistence |
| 📚 **Documented** | OpenAPI 3.1 contract and GitHub Actions CI |

---

## 🚀 Quickstart

```bash
git clone https://github.com/abdullahsamir74/cinema-booking.git
cd cinema-booking
docker compose up --build -d
```

👉 **Open [http://localhost:8080](http://localhost:8080)**

The app, Redis and health checks are already wired together. Click **Admin**
to add a movie; the local demo token is `admin-demo-token`.

```bash
docker compose logs -f app     # follow logs
docker compose down            # stop everything
```

<details>
<summary>Prefer running Go locally?</summary>

```bash
ADMIN_TOKEN=admin-demo-token go run ./cmd/server   # in-memory, zero dependencies
```

To use Redis:

```bash
docker compose up -d redis
BOOKING_STORE=redis REDIS_ADDR=localhost:6380 ADMIN_TOKEN=admin-demo-token go run ./cmd/server
```

Optional Redis admin UI: `docker compose --profile tools up -d` → http://localhost:8081

</details>

---

## 🎟️ Try a booking

```bash
# See the room
curl -s localhost:8080/api/v1/movies/inception/seats | jq '.stats'

# Hold seat A1
curl -s -X POST localhost:8080/api/v1/movies/inception/seats/A1/hold \
  -H 'Content-Type: application/json' \
  -d '{"user_id":"alice-1"}' | jq

# Try to steal it — you get 409 seat_unavailable
curl -s -X POST localhost:8080/api/v1/movies/inception/seats/A1/hold \
  -H 'Content-Type: application/json' \
  -d '{"user_id":"bob-22"}'

# Confirm your hold
curl -s -X POST localhost:8080/api/v1/movies/inception/seats/A1/confirm \
  -H 'X-User-ID: alice-1' | jq
```

---

## 🔌 API

Base URL: `http://localhost:8080/api/v1`

| Method | Endpoint | What it does |
| --- | --- | --- |
| `GET` | `/movies` | List movies |
| `GET` | `/movies/{movieID}` | Get one movie |
| `GET` | `/movies/{movieID}/seats` | Live seat map and stats |
| `GET` | `/movies/{movieID}/bookings` | Active reservations |
| `POST` | `/movies/{movieID}/seats/{seatID}/hold` | Hold a seat |
| `POST` | `/movies/{movieID}/seats/{seatID}/confirm` | Confirm a hold |
| `DELETE` | `/movies/{movieID}/seats/{seatID}/hold` | Release a hold |
| `POST` | `/admin/movies` | Create a movie (Bearer `ADMIN_TOKEN`) |
| `GET` | `/healthz`, `/readyz` | Liveness and readiness |

Full contract: [`docs/openapi.yaml`](docs/openapi.yaml) — import it into Postman, Insomnia or Swagger UI.

---

## 🧠 How it works

```text
Browser (embedded SPA)
        │  REST / JSON
        ▼
HTTP API  →  Booking service  →  Repository port  ─┬─→ Redis  (SET NX + Lua)
                                                      └─→ Memory (mutex)
```

The important part:

1. **Hold** is `SET key value NX PX ttl` — Redis decides the winner atomically.
2. **Confirm** runs a Lua script that checks ownership and flips `held → confirmed`.
3. **Release** deletes the hold only if the caller owns it.
4. If nobody confirms, the TTL returns the seat to the pool automatically.

The result: no double bookings, no stale locks, and safe horizontal scaling.

---

## ➕ Adding a movie

No rebuild required. Click **Admin** in the header, enter the admin token and
fill in the title, slug, room size and details. The movie appears in the
catalogue immediately and its seats become bookable.

### 🔑 What is `ADMIN_TOKEN`?

It is a **shared password for catalogue writes**, not a user account. The
server reads it once at startup and compares it with the
`Authorization: Bearer <token>` header on admin requests.

- **Local default:** `admin-demo-token` (used by Docker Compose and `make run`)
- **Custom value:** set `ADMIN_TOKEN=my-secret-123` in `.env` or your shell, then restart
- **Disabled:** leave `ADMIN_TOKEN` empty
- **Minimum length:** 8 characters
- **Browser storage:** `sessionStorage`, so it disappears when the tab closes

Change it before exposing the app publicly — `admin-demo-token` is only a demo
default.

```bash
# .env
ADMIN_TOKEN=my-secret-123
```

```bash
docker compose up --build -d
```

Then enter `my-secret-123` in the Admin dialog.

### 🌐 Or use the API directly

```bash
curl -X POST localhost:8080/api/v1/admin/movies \
  -H 'Authorization: Bearer admin-demo-token' \
  -H 'Content-Type: application/json' \
  -d '{
    "id": "my-movie",
    "title": "My Movie",
    "description": "A short description.",
    "genre": "Drama",
    "rating": "PG-13",
    "runtime_minutes": 120,
    "rows": 5,
    "seats_per_row": 8
  }'
```

`id` is a URL slug, so `My Movie!` is normalised to `my-movie`. In Redis mode
custom movies survive restarts; in memory mode they last for the process
lifetime. The built-in catalogue stays in
[`internal/catalog/catalog.go`](internal/catalog/catalog.go).

---

## 🧪 Tests

```bash
make test          # unit + HTTP tests
make race          # everything under the race detector
make cover         # coverage summary
make integration   # Redis integration tests (Redis on :6380)
```

CI runs formatting, vet, race tests, coverage, real-Redis integration tests and a Docker image build.

---

## 📁 Layout

```text
cmd/server/            composition root + graceful shutdown
internal/domain/       entities, movie validation, seat rules, errors
internal/booking/      booking use cases + repository/catalogue ports
internal/catalog/      seed catalogue, merge/find helpers, catalogue port
internal/adapters/     memory and Redis booking + movie stores
internal/config/       environment configuration and validation
internal/httpapi/      routes, middleware, embedded admin UI
docs/openapi.yaml      API contract
.github/workflows/     CI pipeline
```

---

## ⚙️ Configuration

Copy [`.env.example`](.env.example) and adjust as needed.

| Variable | Default | Purpose |
| --- | --- | --- |
| `APP_ADDR` | `:8080` | HTTP listen address |
| `BOOKING_STORE` | `memory` | `memory` or `redis` |
| `REDIS_ADDR` | `localhost:6379` | Redis address |
| `HOLD_TTL` | `2m` | How long a seat stays held |
| `ADMIN_TOKEN` | empty (off) | Enables the Admin API and dialog; ≥ 8 characters |
| `CORS_ALLOWED_ORIGINS` | `*` | Allowed browser origins |
| `LOG_LEVEL` | `info` | `debug`, `info`, `warn`, `error` |

---

<div align="center">

Built with Go, Redis and a healthy respect for race conditions.

**[⭐ Star the repo](https://github.com/abdullahsamir74/cinema-booking)** · [MIT License](LICENSE)

</div>
