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
  `SVR,FFW,TOR,WSW`
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

SMS/email remain future roadmap items, but the active delivery pipeline is
voice-only right now.

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
internal/providers/          Twilio provider integration
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

### AMD64 Linux Builds

The four custom Go service images are built as static Linux binaries and the
Dockerfile is cross-platform aware:

- builder stage runs on the native build machine via `$BUILDPLATFORM`
- Go binaries are compiled for the requested target via `TARGETOS` and
  `TARGETARCH`
- final runtime image inherits the requested target platform automatically

That means:

- `docker compose up --build` on an Apple Silicon Mac builds local `arm64`
  images for local development
- the same repo can build deployable `linux/amd64` images for an x86_64 Linux
  server
- the bundled `mysql:8.4`, `redis:7.4-alpine`, and `willfarrell/autoheal`
  images are official multi-arch images and also run on `linux/amd64`

To build all four custom service images for an amd64 Linux host from this Mac:

```bash
./ops/build-linux-amd64-images.sh
```

That script uses Docker Buildx and produces:

- `thundercall-api:amd64`
- `thundercall-ingest:amd64`
- `thundercall-worker:amd64`
- `thundercall-voice-dispatcher:amd64`

You can also build an individual target directly:

```bash
docker buildx build --platform linux/amd64 --target api --tag thundercall-api:amd64 --load .
docker buildx build --platform linux/amd64 --target ingest --tag thundercall-ingest:amd64 --load .
docker buildx build --platform linux/amd64 --target worker --tag thundercall-worker:amd64 --load .
docker buildx build --platform linux/amd64 --target voice-dispatcher --tag thundercall-voice-dispatcher:amd64 --load .
```

To verify the resulting image architecture:

```bash
docker image inspect thundercall-api:amd64 --format '{{.Architecture}}/{{.Os}}'
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

### Logging

| Variable | Default | Notes |
| --- | --- | --- |
| `THUNDERCALL_LOG_LEVEL` | `info` | Supported values: `debug`, `info`, `warn`, `error`. |

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
| `THUNDERCALL_NWWS_PRODUCTS` | `SVR,FFW,TOR,WSW` | Allowed raw NWWS/AWIPS products persisted by ingest. Matching is exact, so `SVS` is ignored unless `SVS` itself is configured. |
| `THUNDERCALL_NWWS_LOG_FULL_MESSAGES` | `false` | When `true`, emits full NWWS bulletin text at `DEBUG` level. |
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
| `THUNDERCALL_API_PUBLIC_SIGNUP_RATE_LIMIT_COUNT` | `10` | Max public signup POSTs allowed per client per window. |
| `THUNDERCALL_API_PUBLIC_SIGNUP_RATE_LIMIT_WINDOW` | `1m` | Public signup rate-limit window. Set count or window to `0` to disable app-side signup throttling. |
| `THUNDERCALL_API_PUBLIC_SIGNUP_PROXY_SHARED_SECRET` | empty | Optional shared secret required for signed public signup proxy requests. When set, unsigned direct browser requests to the new signup endpoint are rejected with `401`. |
| `THUNDERCALL_API_PUBLIC_SIGNUP_PROXY_MAX_SKEW` | `5m` | Max allowed clock skew for signed public signup proxy timestamps. |

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
| `TWILIO_VOICE_STATUS_CALLBACK` | `https://api.thundercall.com/api/providers/twilio/voice/status` | Twilio voice status callback URL. |
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
| `THUNDERCALL_LIVE_TWILIO_CALLBACK_URL` | empty | Optional public callback URL for the live test. When set, the live test also validates Twilio webhook persistence end to end. Use the full path, usually `/api/providers/twilio/voice/status`. |
| `THUNDERCALL_LIVE_TWILIO_CALLBACK_BIND_ADDR` | `:18080` | Local bind address for the temporary callback server started by the live test. Point your public tunnel/domain at this port. |
| `THUNDERCALL_LIVE_TWILIO_CALLBACK_TIMEOUT` | `2m` | How long the live test waits for Twilio to deliver the final webhook callback. |

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

The public signup handler is rate-limited per client IP by default and returns
`429 Too Many Requests` with a `Retry-After` header when the limit is exceeded.
If the upstream geocoder has a transient failure, signup still succeeds and the
location is stored with enrichment pending. True no-match addresses still
return `422 Unprocessable Entity`.

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
  - places one real Twilio call, verifies the later update is suppressed for the same recipient, and when `THUNDERCALL_LIVE_TWILIO_CALLBACK_URL` is set also verifies the real Twilio webhook updates `delivery_attempts`, `users_messages`, and `notifications`

### DB-Backed Integration-Style Test

This test is not build-tagged, but it still uses the disposable MySQL harness:

- `internal/httpapi/public_signup_integration_test.go`
  - `TestHandlePublicSignupCreatesUserLocationContactsAndSettings`
  - verifies the public signup flow creates the user, location, contact
    methods, subscription, and warning settings correctly
  - `TestHandlePublicSignupCreatesPendingLocationWhenResolverFailsUpstream`
  - verifies public signup still creates the user/location when the upstream
    resolver fails and leaves geodata empty for later enrichment
  - `TestHandlePublicSignupRejectsAddressNoMatch`
  - verifies a true address no-match still fails and does not create records
  - `TestHandlePublicSignupEnrichesPendingLocationWhenResolverRecovers`
  - verifies a later successful signup enriches the previously pending
    location instead of creating a second one
  - `TestHandlePublicSignupCreatesLocationWhenEnrichmentIsPartial`
  - verifies a partially enriched location is still accepted and preserves the
    resolved pieces that were available

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

Live Twilio integration test with real callback validation:

```bash
THUNDERCALL_RUN_LIVE_TWILIO_TEST=1 \
THUNDERCALL_LIVE_TWILIO_TEST_TO=+14075551212 \
THUNDERCALL_LIVE_TWILIO_CALLBACK_URL='https://your-public-host.example/api/providers/twilio/voice/status' \
THUNDERCALL_LIVE_TWILIO_CALLBACK_BIND_ADDR=':18080' \
THUNDERCALL_LIVE_TWILIO_CALLBACK_TIMEOUT='2m' \
THUNDERCALL_TEST_MYSQL_DSN='root:root@tcp(127.0.0.1:3306)/thundercall_test?charset=utf8mb4&parseTime=true&loc=UTC' \
TWILIO_ACCOUNT_SID=... \
TWILIO_AUTH_TOKEN=... \
TWILIO_VOICE_FROM=... \
GOCACHE=/tmp/go-build \
go test -tags integration ./internal/voicedispatcher -run TestLiveTwilioCallsInitialEventAndSuppressesEXTForSameRecipient -count=1 -v
```

Notes:

- the test starts a temporary local callback server on `THUNDERCALL_LIVE_TWILIO_CALLBACK_BIND_ADDR`
- your public callback URL must forward to that local port and preserve forwarded host/proto headers for signature validation
- the live callback assertion accepts either a completed call or a final failed outcome such as `busy`, `no-answer`, or `canceled`, but it requires the callback rows to be persisted consistently

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

- add operator write flows beyond basic user/location creation
- add SMS/email execution only when product scope calls for it
- continue validating Go-vs-legacy recipient parity during cutover
