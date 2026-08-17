# thundercall-go

`thundercall-go` is a standalone rewrite of ThunderCall focused on a split
runtime:

- `api` exposes operator-facing authentication and read APIs for alert history,
  delivery metrics, impacted locations, and results drill-downs
- `ingest` connects to NWWS over XMPP, parses messages, persists normalized
  alerts, and publishes work to Redis Streams
- `worker` consumes queued alerts, resolves recipients, and delivers
  SMS/voice/email through Twilio and SendGrid
- MySQL holds the source-of-truth data model for accounts, users, locations,
  messages, and deliveries

## Why This Repo Exists

The legacy implementation couples inbound weather alerts to OneHub-specific
concepts like `Record`, `RecordLocation`, `CompanyThunderCall`, `MsgrJob`, and
`RecordAccessList`.

This repo starts the separation by:

- renaming `Record` to `user`
- renaming `Company` tenancy concerns to `account`
- collapsing delivery orchestration into a direct `message -> user_messages ->
  delivery_attempts` flow

## What Is Implemented Today

- NWWS XMPP consumer in Go using `mellium.im/xmpp`
- NWWS parsing and normalization based on the legacy fixtures and reference PDFs
- MySQL persistence for `source_messages`, `messages`, `users_messages`, and
  `delivery_attempts`
- Redis Streams outbox publishing and worker consumption
- Twilio SMS/voice and SendGrid email provider integrations
- Operator API with login/logout, bearer-token sessions, message history,
  delivery summaries, message results, and location listing endpoints
- NWWS-focused parser tests covering segmented, non-segmented, and severe
  weather products

## Project Layout

```text
cmd/api/                operator API binary
cmd/ingest/             NWWS ingest binary
cmd/worker/             delivery worker binary
internal/config/        env-based configuration
internal/database/      MySQL connection setup
internal/events/        queue event payloads
internal/httpapi/       auth/session handling and operator API endpoints
internal/ingest/        NWWS ingest pipeline and outbox relay
internal/models/        MySQL-backed resource models
internal/nwws/          NWWS parser, normalization, fixtures, tests
internal/queue/         Redis Streams queue client
internal/repositories/  resource repositories, including API auth tables
internal/thundercall/   shared delivery and recipient-resolution logic
internal/worker/        queued delivery execution
db/schema.sql           initial MySQL InnoDB schema
```

## Running Locally

```bash
go run ./cmd/api
go run ./cmd/ingest
go run ./cmd/worker
```

## Running With Docker

Builds for the three service binaries live in a single multi-stage
[Dockerfile](/Users/ernie/Projects/VOLO/ThunderCall/thundercall-go/Dockerfile).
The compose stack in
[compose.yaml](/Users/ernie/Projects/VOLO/ThunderCall/thundercall-go/compose.yaml)
starts:

- `mysql` with the schema from
  [db/schema.sql](/Users/ernie/Projects/VOLO/ThunderCall/thundercall-go/db/schema.sql)
- `redis`
- `api`
- `worker`
- `ingest` behind the optional `nwws` profile, since it requires live NWWS
  credentials

Set up the Docker env file:

```bash
cp .env.docker.example .env.docker
```

Start the default local stack:

```bash
docker compose up --build
```

Start the NWWS ingest service too:

```bash
docker compose --profile nwws up --build
```

Useful commands:

```bash
# Create an API operator user inside the api image.
docker compose run --rm api create-user \
  --account-id 1 \
  --email admin@example.com \
  --password 'change-me' \
  --display-name 'ThunderCall Admin'

# Build a single service image.
docker build --target api -t thundercall-api .
docker build --target ingest -t thundercall-ingest .
docker build --target worker -t thundercall-worker .
```

Notes:

- `api` is exposed on `http://localhost:8080`
- `mysql` is exposed on `localhost:3306`
- `redis` is exposed on `localhost:6379`
- `worker` can start without Twilio/SendGrid credentials, but delivery attempts
  will fail until those providers are configured, except voice calls when
  `THUNDERCALL_TWILIO_VOICE_LOG_ONLY=true` (the current default), which are
  logged and marked sent without calling Twilio
- `ingest` will exit immediately unless the NWWS credentials in `.env.docker`
  are populated

Create an operator login:

```bash
go run ./cmd/api create-user \
  --account-id 1 \
  --email admin@example.com \
  --password 'change-me' \
  --display-name 'ThunderCall Admin'
```

Environment variables:

- `THUNDERCALL_MYSQL_DSN` enables the MySQL-backed runtime
- `THUNDERCALL_API_LISTEN_ADDR`, `THUNDERCALL_API_SESSION_TTL`
- `THUNDERCALL_REDIS_ADDR`, `THUNDERCALL_REDIS_STREAM`,
  `THUNDERCALL_REDIS_GROUP`, `THUNDERCALL_REDIS_CONSUMER`
- `THUNDERCALL_TWILIO_VOICE_LOG_ONLY` keeps worker voice delivery in log-only
  mode for validation runs without placing real calls
- `THUNDERCALL_NWWS_USERNAME`, `THUNDERCALL_NWWS_PASSWORD`,
  `THUNDERCALL_NWWS_DOMAIN`, `THUNDERCALL_NWWS_ROOM_SERVER`,
  `THUNDERCALL_NWWS_ROOM`, `THUNDERCALL_NWWS_PRODUCTS`
- `TWILIO_ACCOUNT_SID`, `TWILIO_AUTH_TOKEN`
- `TWILIO_SMS_FROM` or `TWILIO_MESSAGING_SERVICE_SID`
- `TWILIO_VOICE_FROM`
- `SENDGRID_API_KEY`, `SENDGRID_FROM_EMAIL`, `SENDGRID_FROM_NAME`

## MySQL Integration Tests

The repo includes opt-in MySQL integration tests for the real spatial and
delivery-dedupe path, including:

- `locations.MatchForMessage(...)` against MySQL `ST_Intersects(...)`
- recipient resolution from real `locations` + `users_locations` rows
- same-event update behavior where a later polygon only calls newly affected
  users

These tests require a disposable MySQL database DSN with permission to create
and drop test databases. The safest pattern is to point at the local Docker
MySQL root account:

```bash
THUNDERCALL_TEST_MYSQL_DSN='root:root@tcp(127.0.0.1:3306)/thundercall_test?charset=utf8mb4&parseTime=true&loc=UTC' \
  go test -tags=integration ./internal/repositories/locations ./internal/thundercall
```

The harness creates a unique database per test run and drops it on cleanup. As
a guardrail, the DSN database name must include `test`.

## Comparing Go Vs Legacy NWWS

Use the comparison tool to diff recent NWWS messages in Go `messages` against
legacy `PendingMessages` over the same time window:

```bash
go run ./cmd/compare-nwws -window 30m
```

Useful flags:

- `-since 2026-08-11T18:00:00Z`
- `-until 2026-08-11T18:30:00Z`
- `-limit 1000`
- `-strict` to exit non-zero when differences are found

Defaults:

- Go reads `THUNDERCALL_MYSQL_DSN`, falling back to
  `thundercall:thundercall@tcp(127.0.0.1:3306)/thundercall?...`
- legacy reads `THUNDERCALL_LEGACY_PG_DSN`
- if no legacy DSN is set, the tool falls back to the C# repo defaults:
  `postgres://postgres:postgres@10.0.1.199:5432/thundercall?sslmode=prefer`

## Schema Notes

The schema in [db/schema.sql](/Users/ernie/Projects/VOLO/ThunderCall/thundercall-go/db/schema.sql)
tracks the simplified model:

- `accounts`
- `users`
- `user_contact_methods`
- `locations`
- `users_locations`
- `account_settings`
- `users_settings`
- `api_users`
- `api_sessions`
- `source_messages`
- `messages`
- `users_messages`
- `delivery_attempts`
- `outbox_events`

## API Endpoints

Authenticated requests use `Authorization: Bearer <token>`.

- `POST /v1/auth/login`
- `POST /v1/auth/logout`
- `GET /v1/auth/me`
- `GET /v1/dashboard/summary`
- `GET /v1/messages`
- `GET /v1/messages/{id}`
- `GET /v1/messages/{id}/locations`
- `GET /v1/messages/{id}/deliveries`
- `GET /v1/locations`
- `GET /v1/locations/{id}`

### Query Parameters

`GET /v1/messages`

- `from`, `to`: RFC3339 or `YYYY-MM-DD`
- `search`: free-text search across message metadata
- `eventCode`: NWWS/VTEC-style event code filter such as `SVR` or `TOR`
- `messageType`: normalized message type
- `status`: processing status
- `source`: source feed, currently `nwws`
- `limit`, `offset`: pagination

`GET /v1/dashboard/summary`

- Uses the same filter set as `GET /v1/messages`

`GET /v1/locations`

- `search`: free-text location name/address search
- `activeOnly`: `true` or `false`
- `limit`, `offset`: pagination

`GET /v1/messages/{id}/deliveries`

- `search`: recipient name/title search
- `status`: recipient delivery status
- `limit`, `offset`: pagination

The initial API is intentionally operator-focused and read-heavy. It is meant
to support the first replacement UI for:

- historical message browsing
- per-message SMS/email/voice counts
- entries, attempts, and sent/failure rollups
- impacted-location drill-downs
- recipient/result inspection for a selected alert

## Recommended Next Steps

1. Add external migration tooling from the legacy ThunderCall/1Hub schema into
   this simplified schema.
2. Add delivery status callback/webhook handling for Twilio and SendGrid.
3. Add write endpoints for operator workflows such as managing locations,
   operator users, and notification settings.
4. Add integration tests around MySQL + Redis-backed ingest/worker/API flows.
5. Expand beyond NWWS only after the new pipeline is stable.
