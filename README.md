# thundercall-go

`thundercall-go` is the standalone ThunderCall runtime. It is currently built
around four Go processes plus MySQL and Redis:

- `api`: operator API, public signup API, user/location creation, and
  message-lookup endpoints
- `ingest`: NWWS XMPP consumer, parser, normalizer, and outbox publisher
- `worker`: recipient resolution and delivery-attempt planning
- `voice-dispatcher`: paced Twilio voice execution and retry handling
- `mysql`: source-of-truth data store
- `redis`: queue transport for accepted-message work

## Current Product Scope

Implemented today:

- NWWS-only ingest
- Configurable allowed NWWS products, currently defaulting to
  `SVR,FFW,TOR,WSW,TSU`
- Persistence for `source_messages`, `nws_events`, `messages`,
  `notifications`, `users_messages`, `delivery_attempts`, and
  `outbox_events`
- Polygon/FIPS/NWS-zone recipient resolution
- Event-aware dedupe so later updates only call net-new recipients for the
  same event/channel
- Twilio voice delivery with:
  - dry-run mode
  - single-number override mode
  - optional single-call collapse in override mode
  - paced dispatch with configurable CPS
- Public signup flow for station forms
- Operator API for auth, dashboard summaries, message history, message
  lookups, and location inspection
- Docker health checks and host-run MySQL backup script

Not active yet:

- SMS execution
- email execution

There is still some placeholder/provider code for SMS/email, but the running
pipeline is voice-first right now.

## Project Layout

```text
cmd/api/                     API binary
cmd/ingest/                  ingest binary
cmd/voice-dispatcher/        voice dispatcher binary
cmd/worker/                  worker binary
db/schema.sql                current bootstrap schema
docs/                        reference docs
internal/config/             env-based config loading
internal/database/           MySQL connection setup
internal/events/             queue event payloads
internal/geocode/            Census + weather.gov lookups
internal/health/             heartbeat and health-check helpers
internal/httpapi/            HTTP handlers and query layer
internal/ingest/             ingest pipeline and outbox relay
internal/models/             shared data models
internal/nwws/               parser, normalization, fixtures
internal/providers/          Twilio and SendGrid providers
internal/queue/redisstreams/ Redis Streams client
internal/repositories/       table repositories
internal/testmysql/          disposable MySQL test harness
internal/thundercall/        recipient resolution and planning logic
internal/voicedispatcher/    voice claim/pacing/send flow
internal/worker/             worker queue consumer
ops/                         backup and cron examples
API.md                       request/response details for HTTP API
compose.yaml                 local Docker stack
Dockerfile                   multi-stage image build
```

## Commands

The repo currently ships these binaries:

- `go run ./cmd/api`
  - default command: serve API
  - subcommands:
    - `create-user`
    - `healthcheck`
- `go run ./cmd/ingest`
  - default command: start NWWS ingest loop
  - subcommands:
    - `healthcheck`
- `go run ./cmd/worker`
  - default command: start Redis-backed planning worker
  - subcommands:
    - `healthcheck`
- `go run ./cmd/voice-dispatcher`
  - default command: claim queued voice attempts and call Twilio
  - subcommands:
    - `healthcheck`

## Running Locally

Start each process directly:

```bash
go run ./cmd/api
go run ./cmd/ingest
go run ./cmd/worker
go run ./cmd/voice-dispatcher
```

Create an API operator user:

```bash
go run ./cmd/api create-user \
  --account-id 1 \
  --email admin@example.com \
  --password 'change-me' \
  --display-name 'ThunderCall Admin'
```

## Running With Docker

Copy the example env file:

```bash
cp .env.docker.example .env.docker
```

Start the default local stack:

```bash
docker compose up --build
```

That starts:

- `mysql`
- `redis`
- `api`
- `worker`
- `voice-dispatcher`
- `autoheal`

Start NWWS ingest too:

```bash
docker compose --profile nwws up --build
```

Useful commands:

```bash
docker compose run --rm api create-user \
  --account-id 1 \
  --email admin@example.com \
  --password 'change-me' \
  --display-name 'ThunderCall Admin'

docker build --target api -t thundercall-api .
docker build --target ingest -t thundercall-ingest .
docker build --target worker -t thundercall-worker .
docker build --target voice-dispatcher -t thundercall-voice-dispatcher .
```

Local Docker defaults:

- API: `http://localhost:8080`
- MySQL: `127.0.0.1:3306`
- Redis: `127.0.0.1:6379`
- ingest is behind the `nwws` compose profile
- worker and voice-dispatcher can run without live Twilio credentials if
  voice dry-run mode is enabled

## Runtime Environment Variables

These are the env vars loaded by `internal/config`.

### MySQL

| Variable | Default | Notes |
| --- | --- | --- |
| `THUNDERCALL_MYSQL_DSN` | none | Required for all four runtime services. |

### Redis

| Variable | Default | Notes |
| --- | --- | --- |
| `THUNDERCALL_REDIS_ADDR` | none | Required for `ingest` and `worker`. |
| `THUNDERCALL_REDIS_PASSWORD` | empty | Optional Redis auth. |
| `THUNDERCALL_REDIS_DB` | `0` | Redis DB index. |
| `THUNDERCALL_REDIS_STREAM` | `thundercall:messages` | Message stream key. |
| `THUNDERCALL_REDIS_GROUP` | `thundercall-workers` | Consumer group name. |
| `THUNDERCALL_REDIS_CONSUMER` | `os.Hostname()` or `worker` | Default consumer name. |
| `THUNDERCALL_REDIS_BLOCK` | `5s` | Blocking read duration for worker stream reads. |
| `THUNDERCALL_REDIS_CLAIM_IDLE` | `30s` | Minimum idle before pending messages may be auto-claimed. |
| `THUNDERCALL_REDIS_BATCH_SIZE` | `25` | General Redis batch size default. |

### NWWS

| Variable | Default | Notes |
| --- | --- | --- |
| `THUNDERCALL_NWWS_DOMAIN` | `nwws-oi.weather.gov` | XMPP domain. |
| `THUNDERCALL_NWWS_ROOM_SERVER` | `conference.nwws-oi.weather.gov` | XMPP MUC server. |
| `THUNDERCALL_NWWS_ROOM` | `nwws` | Room name. |
| `THUNDERCALL_NWWS_USERNAME` | none | Required for ingest runtime. |
| `THUNDERCALL_NWWS_PASSWORD` | none | Required for ingest runtime. |
| `THUNDERCALL_NWWS_JOIN_PASSWORD` | same as `THUNDERCALL_NWWS_PASSWORD` | Optional separate room password. |
| `THUNDERCALL_NWWS_NICK` | same as `THUNDERCALL_NWWS_USERNAME` | Optional MUC nick override. |
| `THUNDERCALL_NWWS_PRODUCTS` | `SVR,FFW,TOR,WSW,TSU` | Allowed products persisted by ingest. |
| `THUNDERCALL_NWWS_LOG_FULL_MESSAGES` | `false` | Log complete NWWS bulletin text. |
| `THUNDERCALL_NWWS_IDLE_TIMEOUT` | `5m` | Consumer idle timeout watchdog. |

### Ingest

| Variable | Default | Notes |
| --- | --- | --- |
| `THUNDERCALL_INGEST_PUBLISH_INTERVAL` | `2s` | How often the outbox relay wakes up. |
| `THUNDERCALL_INGEST_PUBLISH_BATCH_SIZE` | `50` | Max unpublished outbox rows pushed per batch. |

### Worker

| Variable | Default | Notes |
| --- | --- | --- |
| `THUNDERCALL_WORKER_READ_COUNT` | `25` | Max messages read from Redis per worker pass. |

### Voice Dispatcher

| Variable | Default | Notes |
| --- | --- | --- |
| `THUNDERCALL_VOICE_CONSUMER` | `os.Hostname()` or `voice-dispatcher` | Consumer identity stored on claimed attempts. |
| `THUNDERCALL_VOICE_CLAIM_BATCH_SIZE` | `25` | Max queued voice attempts claimed per pass. |
| `THUNDERCALL_VOICE_CPS` | `1` | Pacing rate used by the voice dispatcher. |
| `THUNDERCALL_VOICE_CLAIM_LEASE` | `2m` | Lease expiration on claimed voice attempts. |
| `THUNDERCALL_VOICE_RETRY_DELAY` | `30s` | Requeue delay for retryable Twilio failures. |
| `THUNDERCALL_VOICE_IDLE_SLEEP` | `2s` | Sleep interval when no voice work is available. |

### API

| Variable | Default | Notes |
| --- | --- | --- |
| `THUNDERCALL_API_LISTEN_ADDR` | `:8080` | API listen address. |
| `THUNDERCALL_API_SESSION_TTL` | `24h` | Bearer-session TTL. |

### Health Checks

| Variable | Default | Notes |
| --- | --- | --- |
| `THUNDERCALL_HEARTBEAT_PATH` | empty | Required if you want heartbeat-based health checks. |
| `THUNDERCALL_HEARTBEAT_MAX_AGE` | `1m` | Max allowed heartbeat age. |

### Geocoding

| Variable | Default | Notes |
| --- | --- | --- |
| `THUNDERCALL_CENSUS_BASE_URL` | `https://geocoding.geo.census.gov/geocoder` | U.S. Census geocoder base URL. |
| `THUNDERCALL_CENSUS_BENCHMARK` | `Public_AR_Current` | Census benchmark. |
| `THUNDERCALL_CENSUS_VINTAGE` | `Current_Current` | Census vintage. |
| `THUNDERCALL_WEATHERGOV_BASE_URL` | `https://api.weather.gov` | Used for zone lookup. |
| `THUNDERCALL_GEOCODER_USER_AGENT` | `thundercall/0.1` | Sent to upstream geocoding services. |
| `THUNDERCALL_GEOCODER_TIMEOUT` | `10s` | Outbound geocoder timeout. |

### Twilio

| Variable | Default | Notes |
| --- | --- | --- |
| `TWILIO_ACCOUNT_SID` | empty | Required for live Twilio API calls. |
| `TWILIO_AUTH_TOKEN` | empty | Required for live Twilio API calls. |
| `TWILIO_MESSAGING_SERVICE_SID` | empty | Reserved for future SMS support. |
| `TWILIO_SMS_FROM` | empty | Reserved for future SMS support. |
| `TWILIO_VOICE_FROM` | empty | Required for live voice calls. |
| `TWILIO_VOICE_URL` | empty | Optional hosted Twilio Function base URL. |
| `TWILIO_VOICE_STATUS_CALLBACK` | empty | Optional Twilio status callback URL. |
| `THUNDERCALL_TWILIO_VOICE_TO_OVERRIDE` | empty | Force all test calls to one destination. |
| `THUNDERCALL_TWILIO_VOICE_OVERRIDE_SINGLE_CALL` | `true` | Collapse override mode to one real call per message/event channel window. |
| `THUNDERCALL_TWILIO_VOICE_LOG_ONLY` | `true` | Dry-run voice mode. No real Twilio call is placed. |

Twilio voice behavior:

- if `TWILIO_VOICE_URL` is set and override mode is off, the dispatcher calls
  the hosted Twilio Function with `audio=<warning family>` and
  `id=<account_id>`
- if override mode is on, the dispatcher intentionally falls back to inline
  TwiML so the test call can announce the intended recipient
- if log-only mode is on, attempts are marked sent with deterministic dry-run
  provider IDs and no real call is placed

### SendGrid

| Variable | Default | Notes |
| --- | --- | --- |
| `SENDGRID_API_KEY` | empty | Reserved for future email execution. |
| `SENDGRID_FROM_EMAIL` | empty | Reserved for future email execution. |
| `SENDGRID_FROM_NAME` | empty | Reserved for future email execution. |

## Test-Only Environment Variables

### Disposable MySQL Harness

| Variable | Default | Notes |
| --- | --- | --- |
| `THUNDERCALL_TEST_MYSQL_DSN` | none | Required for MySQL-backed tests. DB name must include `test`. The harness creates and drops a unique database per run. |

### Redis-Backed Pipeline Tests

| Variable | Default | Notes |
| --- | --- | --- |
| `THUNDERCALL_TEST_REDIS_ADDR` | `127.0.0.1:6379` fallback if `THUNDERCALL_REDIS_ADDR` is unset | Used by Redis-backed integration tests. |
| `THUNDERCALL_TEST_REDIS_PASSWORD` | fallback to `THUNDERCALL_REDIS_PASSWORD`, otherwise empty | Optional auth for Redis-backed tests. |

### Live Twilio Integration Test

| Variable | Default | Notes |
| --- | --- | --- |
| `THUNDERCALL_RUN_LIVE_TWILIO_TEST` | disabled | Must be `1` or `true` to place a real call. |
| `THUNDERCALL_LIVE_TWILIO_TEST_TO` | none | Destination for the real test call. |

## Backup Script Environment Variables

These are used by `ops/mysql-backup.sh`.

| Variable | Default |
| --- | --- |
| `THUNDERCALL_BACKUP_COMPOSE_FILE` | `<repo>/compose.yaml` |
| `THUNDERCALL_BACKUP_MYSQL_SERVICE` | `mysql` |
| `THUNDERCALL_BACKUP_MYSQL_DATABASE` | `thundercall` |
| `THUNDERCALL_BACKUP_MYSQL_USER` | `thundercall` |
| `THUNDERCALL_BACKUP_MYSQL_PASSWORD` | `thundercall` |
| `THUNDERCALL_BACKUP_DIR` | `<repo>/backups/mysql` |
| `THUNDERCALL_BACKUP_RETENTION_DAYS` | `14` |
| `THUNDERCALL_BACKUP_FILE_PREFIX` | `thundercall` |

## HTTP API Surface

See `API.md` for request/response bodies. Current routes:

### Health

- `GET /healthz`

### Public Signup Aliases

These all map to the same public signup flow:

- `POST /api/users/signup`
- `POST /api/products/{productId}/records`
- `POST /v1/public/signups`

### Authenticated Operator API

- `POST /v1/auth/login`
- `POST /v1/auth/logout`
- `GET /v1/auth/me`
- `GET /v1/dashboard/summary`
- `GET /v1/messages`
- `POST /v1/messages/lookup`
- `GET /v1/messages/{id}`
- `GET /v1/messages/{id}/locations`
- `GET /v1/messages/{id}/deliveries`
- `GET /v1/locations`
- `GET /v1/locations/{id}`
- `POST /v1/users`

Useful notes:

- `POST /v1/messages/lookup` finds warnings by address or lat/lon
- `POST /v1/users` creates a user plus geocoded location data under an account
- `POST /api/products/{productId}/records` is kept as a legacy-friendly alias
  for older signup forms

## Schema Summary

Current primary tables:

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
- `nws_events`
- `messages`
- `notifications`
- `users_messages`
- `delivery_attempts`
- `outbox_events`

## Integration Tests

### Tagged Integration Tests

Run with `go test -tags integration ...`.

- `internal/repositories/locations/repository_integration_test.go`
  - `TestRepositoryMatchForMessagePolygonAndFallback`
  - verifies real MySQL spatial matching and FIPS/zone fallback matching
- `internal/thundercall/channel_dispatcher_integration_test.go`
  - `TestMySQLIntegrationInitialAndUpdatedPolygonCallOnlyNetNewUsers`
  - verifies initial-vs-updated event behavior and net-new user suppression
- `internal/voicedispatcher/service_integration_test.go`
  - `TestMySQLIntegrationClaimQueuedVoiceAttemptsFairAcrossMessages`
  - verifies fair SQL claiming across multiple queued messages
- `internal/voicedispatcher/service_integration_test.go`
  - `TestMySQLIntegrationWorkerAndVoiceDispatcherOnlyCallNetNewUsersOnUpdatedEvent`
  - verifies worker planning plus voice-dispatch send/suppress behavior using a fake sender
- `internal/voicedispatcher/pipeline_integration_test.go`
  - `TestIntegrationIngestOutboxWorkerVoiceDispatcherPipeline`
  - full ingest -> outbox -> Redis -> worker -> voice-dispatcher -> DB assertion flow using a real NWWS sample
- `internal/voicedispatcher/pipeline_integration_test.go`
  - `TestIntegrationVoiceDispatcherRespectsCPSAndFairnessAcrossMessages`
  - verifies paced dispatch and cross-message fairness under concurrent queued work
- `internal/voicedispatcher/live_twilio_integration_test.go`
  - `TestLiveTwilioCallsInitialEventAndSuppressesEXTForSameRecipient`
  - places one real Twilio call, then verifies the later update is suppressed for the same recipient

### DB-Backed Integration-Style Test

This test is not build-tagged, but it still uses the disposable MySQL harness:

- `internal/httpapi/public_signup_integration_test.go`
  - `TestHandlePublicSignupCreatesUserLocationContactsAndSettings`
  - verifies the public signup flow creates the user, location, contact
    methods, subscription, and warning settings correctly

### Useful Test Commands

Standard unit tests:

```bash
GOCACHE=/tmp/go-build go test ./...
```

MySQL + Redis integration suite:

```bash
THUNDERCALL_TEST_MYSQL_DSN='root:root@tcp(127.0.0.1:3306)/thundercall_test?charset=utf8mb4&parseTime=true&loc=UTC' \
THUNDERCALL_TEST_REDIS_ADDR=127.0.0.1:6379 \
GOCACHE=/tmp/go-build \
go test -tags integration ./internal/...
```

Live Twilio integration test:

```bash
THUNDERCALL_RUN_LIVE_TWILIO_TEST=1 \
THUNDERCALL_LIVE_TWILIO_TEST_TO=+14075551212 \
THUNDERCALL_TEST_MYSQL_DSN='root:root@tcp(127.0.0.1:3306)/thundercall_test?charset=utf8mb4&parseTime=true&loc=UTC' \
TWILIO_ACCOUNT_SID=... \
TWILIO_AUTH_TOKEN=... \
TWILIO_VOICE_FROM=... \
GOCACHE=/tmp/go-build \
go test -tags integration ./internal/voicedispatcher -run TestLiveTwilioCallsInitialEventAndSuppressesEXTForSameRecipient -count=1 -v
```

## MySQL Backups

Run the host-side backup script:

```bash
./ops/mysql-backup.sh
```

Example overrides:

```bash
THUNDERCALL_BACKUP_DIR=/opt/thundercall/backups/mysql \
THUNDERCALL_BACKUP_RETENTION_DAYS=30 \
THUNDERCALL_BACKUP_MYSQL_PASSWORD='change-me' \
./ops/mysql-backup.sh
```

Sample cron entry:

- `ops/cron/thundercall-mysql-backup.cron.example`

## Near-Term Next Steps

- add Twilio voice status callback ingestion so call outcomes move beyond
  queued/sent/failed
- add operator write flows beyond basic user/location creation
- add SMS/email execution only when product scope calls for it
- continue validating Go-vs-legacy recipient parity during cutover
