# ThunderCall Operator API

This document describes the current HTTP API exposed by the `api` binary in
`thundercall-go`.

Unless noted otherwise:

- request and response bodies are JSON
- timestamps are returned as RFC3339 UTC strings
- errors are returned as `{"error":"..."}` with an appropriate HTTP status
- authenticated endpoints require `Authorization: Bearer <token>`

## Authentication

Authenticated requests use a bearer token created by `POST /v1/auth/login`.

Example header:

```http
Authorization: Bearer tc_example_token_here
```

If the token is missing, revoked, expired, or belongs to an inactive user or
account, the API returns `401 Unauthorized`.

## Common Conventions

### Error Shape

```json
{
  "error": "message not found"
}
```

### Pagination Shape

List endpoints return:

```json
{
  "items": [],
  "page": {
    "total": 0,
    "limit": 50,
    "offset": 0
  }
}
```

### Date Filters

`from` and `to` query parameters accept:

- RFC3339 timestamps, for example `2026-07-31T14:00:00Z`
- `YYYY-MM-DD`, for example `2026-07-31`

When `to` is passed as `YYYY-MM-DD`, it expands to the end of that UTC day.

## Resource Shapes

### User Summary

Returned from auth endpoints:

```json
{
  "id": 1,
  "accountId": 1,
  "email": "admin@example.com",
  "displayName": "ThunderCall Admin",
  "lastLoginAt": "2026-07-31T14:10:00Z",
  "account": {
    "id": 1,
    "name": "Acme Schools"
  }
}
```

### Message Counts

Used on message and dashboard endpoints:

```json
{
  "recipientsCount": 65,
  "attemptsCount": 75,
  "sentRecipientsCount": 57,
  "failedRecipientsCount": 5,
  "partialFailureRecipientsCount": 3,
  "smsAttemptsCount": 65,
  "emailAttemptsCount": 5,
  "voiceAttemptsCount": 5,
  "smsSentCount": 52,
  "emailSentCount": 3,
  "voiceSentCount": 2
}
```

Field meanings:

- `recipientsCount`: number of `users_messages` rows for the message
- `attemptsCount`: number of `delivery_attempts` rows across all channels
- `sentRecipientsCount`: recipients with final `users_messages.status = sent`
- `failedRecipientsCount`: recipients with final `users_messages.status = failed`
- `partialFailureRecipientsCount`: recipients with partial delivery success
- `smsAttemptsCount`, `emailAttemptsCount`, `voiceAttemptsCount`: attempts by channel
- `smsSentCount`, `emailSentCount`, `voiceSentCount`: attempts by channel with `status = sent`

### Message

Returned by `GET /v1/messages` and `GET /v1/messages/{id}`:

```json
{
  "id": 123,
  "source": "NWWS",
  "eventCode": "SVR",
  "messageType": "alert",
  "alertTypeCode": "NEW",
  "title": "Severe Thunderstorm Warning",
  "status": "processed",
  "issuedAt": "2026-07-31T14:00:00Z",
  "receivedAt": "2026-07-31T14:00:05Z",
  "processedAt": "2026-07-31T14:00:06Z",
  "sourceMessageId": 456,
  "externalMessageId": "nwws:abc123",
  "sourceSegmentIndex": 0,
  "polygonWKT": "POLYGON((...))",
  "fipsCodes": ["05143"],
  "nwsZones": ["ARZ103"],
  "counts": {
    "recipientsCount": 65,
    "attemptsCount": 75,
    "sentRecipientsCount": 57,
    "failedRecipientsCount": 5,
    "partialFailureRecipientsCount": 3,
    "smsAttemptsCount": 65,
    "emailAttemptsCount": 5,
    "voiceAttemptsCount": 5,
    "smsSentCount": 52,
    "emailSentCount": 3,
    "voiceSentCount": 2
  }
}
```

Notes:

- `title`, `issuedAt`, `processedAt`, `sourceMessageId`, `externalMessageId`,
  `sourceSegmentIndex`, and `polygonWKT` may be `null` or omitted
- list results are sorted by `COALESCE(issuedAt, receivedAt)` descending, then `id` descending

### Location

Returned by `GET /v1/locations` and `GET /v1/locations/{id}`:

```json
{
  "id": 17,
  "name": "Fayetteville Campus",
  "addressLine1": "123 Main St",
  "addressLine2": null,
  "city": "Fayetteville",
  "stateCode": "AR",
  "postalCode": "72701",
  "countyFips": "05143",
  "nwsZone": "ARZ103",
  "latitude": 36.0626,
  "longitude": -94.1574,
  "coverageWKT": "POLYGON((...))",
  "isThunderCallEnabled": true,
  "active": true,
  "subscribedUsersCount": 44
}
```

Notes:

- `subscribedUsersCount` counts active users with an enabled
  `users_locations` subscription for that location
- list results are sorted by `name`, then `id`

### Message Location

Returned by `GET /v1/messages/{id}/locations`:

```json
{
  "id": 17,
  "name": "Fayetteville Campus",
  "addressLine1": "123 Main St",
  "city": "Fayetteville",
  "stateCode": "AR",
  "countyFips": "05143",
  "nwsZone": "ARZ103",
  "latitude": 36.0626,
  "longitude": -94.1574,
  "coverageWKT": "POLYGON((...))",
  "isThunderCallEnabled": true,
  "active": true,
  "subscribedUsersCount": 0,
  "matchedUsersCount": 44,
  "smsEnabledCount": 40,
  "emailEnabledCount": 18,
  "voiceEnabledCount": 12
}
```

Notes:

- `matchedUsersCount` is the number of recipients matched to that location for the message
- the three `*EnabledCount` fields show per-channel opt-in counts for the matched recipients
- results are sorted by `matchedUsersCount` descending, then location name

### Message Delivery

Returned by `GET /v1/messages/{id}/deliveries`:

```json
{
  "userMessageId": 901,
  "userId": 77,
  "displayName": "Pat Smith",
  "title": "Principal",
  "status": "sent",
  "queuedAt": "2026-07-31T14:00:07Z",
  "deliveredAt": "2026-07-31T14:00:14Z",
  "smsEnabled": true,
  "emailEnabled": true,
  "voiceEnabled": false,
  "matchedLocation": {
    "id": 17,
    "name": "Fayetteville Campus"
  },
  "attempts": [
    {
      "id": 1001,
      "channel": "sms",
      "destination": "+14795551212",
      "provider": "twilio",
      "providerMessageId": "SM123",
      "status": "sent",
      "errorMessage": null,
      "requestedAt": "2026-07-31T14:00:07Z",
      "sentAt": "2026-07-31T14:00:08Z",
      "deliveredAt": "2026-07-31T14:00:14Z"
    }
  ]
}
```

Notes:

- `status` is the final status on the `users_messages` row
- `attempts` are sorted by delivery attempt `id` within a recipient
- list results are sorted by `queuedAt` descending, then `userMessageId` descending

## Endpoints

## `GET /healthz`

Health check for the API process.

Authentication:

- not required

Success response:

```json
{
  "ok": true
}
```

## `POST /v1/auth/login`

Creates a bearer-token session for an operator API user.

Authentication:

- not required

Request body:

```json
{
  "email": "admin@example.com",
  "password": "change-me"
}
```

Success response:

```json
{
  "token": "tc_generated_session_token",
  "expiresAt": "2026-08-01T14:00:00Z",
  "user": {
    "id": 1,
    "accountId": 1,
    "email": "admin@example.com",
    "displayName": "ThunderCall Admin",
    "lastLoginAt": "2026-07-31T14:00:00Z",
    "account": {
      "id": 1,
      "name": "Acme Schools"
    }
  }
}
```

Common errors:

- `400 Bad Request`: invalid JSON body
- `401 Unauthorized`: invalid email or password
- `401 Unauthorized`: account is inactive
- `500 Internal Server Error`: failed to create session or load backing records

## `POST /v1/auth/logout`

Revokes the current bearer-token session.

Authentication:

- required

Request body:

- none

Success response:

```json
{
  "ok": true
}
```

Common errors:

- `401 Unauthorized`: session is required
- `500 Internal Server Error`: failed to revoke session

## `GET /v1/auth/me`

Returns the current authenticated API user and account summary.

Authentication:

- required

Success response:

```json
{
  "user": {
    "id": 1,
    "accountId": 1,
    "email": "admin@example.com",
    "displayName": "ThunderCall Admin",
    "lastLoginAt": "2026-07-31T14:00:00Z",
    "account": {
      "id": 1,
      "name": "Acme Schools"
    }
  }
}
```

Common errors:

- `401 Unauthorized`: session is required

## `GET /v1/dashboard/summary`

Returns rolled-up counts for the current account across all matching messages.

Authentication:

- required

Query parameters:

| Name | Type | Description |
| --- | --- | --- |
| `from` | string | Inclusive lower bound on `issuedAt` or `receivedAt` |
| `to` | string | Inclusive upper bound on `issuedAt` or `receivedAt` |
| `search` | string | Case-insensitive search across event code, message type, title, and body |
| `eventCode` | string | Exact event code filter, normalized to uppercase |
| `messageType` | string | Exact message type filter |
| `status` | string | Exact message status filter |
| `source` | string | Exact source filter, normalized to uppercase, for example `NWWS` |
| `limit` | integer | Accepted but not used by the summary query |
| `offset` | integer | Accepted but not used by the summary query |

Success response:

```json
{
  "messagesCount": 18,
  "recipientsCount": 1024,
  "attemptsCount": 1102,
  "sentRecipientsCount": 988,
  "failedRecipientsCount": 20,
  "partialFailureRecipientsCount": 16,
  "smsAttemptsCount": 900,
  "emailAttemptsCount": 102,
  "voiceAttemptsCount": 100,
  "smsSentCount": 870,
  "emailSentCount": 95,
  "voiceSentCount": 91
}
```

Common errors:

- `400 Bad Request`: invalid query parameter
- `500 Internal Server Error`: failed to load dashboard summary

## `GET /v1/messages`

Returns paginated message history for the authenticated account.

Authentication:

- required

Query parameters:

| Name | Type | Description |
| --- | --- | --- |
| `from` | string | Inclusive lower bound on `issuedAt` or `receivedAt` |
| `to` | string | Inclusive upper bound on `issuedAt` or `receivedAt` |
| `search` | string | Case-insensitive search across event code, message type, title, and body |
| `eventCode` | string | Exact event code filter, normalized to uppercase |
| `messageType` | string | Exact message type filter |
| `status` | string | Exact message status filter |
| `source` | string | Exact source filter, normalized to uppercase, for example `NWWS` |
| `limit` | integer | Page size, min `1`, max `200`, default `50` |
| `offset` | integer | Zero-based row offset, min `0`, default `0` |

Success response:

```json
{
  "items": [
    {
      "id": 123,
      "source": "NWWS",
      "eventCode": "SVR",
      "messageType": "alert",
      "alertTypeCode": "NEW",
      "title": "Severe Thunderstorm Warning",
      "status": "processed",
      "issuedAt": "2026-07-31T14:00:00Z",
      "receivedAt": "2026-07-31T14:00:05Z",
      "processedAt": "2026-07-31T14:00:06Z",
      "sourceMessageId": 456,
      "externalMessageId": "nwws:abc123",
      "sourceSegmentIndex": 0,
      "polygonWKT": "POLYGON((...))",
      "fipsCodes": ["05143"],
      "nwsZones": ["ARZ103"],
      "counts": {
        "recipientsCount": 65,
        "attemptsCount": 75,
        "sentRecipientsCount": 57,
        "failedRecipientsCount": 5,
        "partialFailureRecipientsCount": 3,
        "smsAttemptsCount": 65,
        "emailAttemptsCount": 5,
        "voiceAttemptsCount": 5,
        "smsSentCount": 52,
        "emailSentCount": 3,
        "voiceSentCount": 2
      }
    }
  ],
  "page": {
    "total": 1,
    "limit": 50,
    "offset": 0
  }
}
```

Common errors:

- `400 Bad Request`: invalid query parameter
- `500 Internal Server Error`: failed to load messages

## `POST /v1/messages/lookup`

Resolves either an address or a latitude/longitude pair into a point, county
FIPS code, and NWS zone, then returns matching ThunderCall messages. A message
matches when at least one of these is true:

- the point falls inside the message polygon
- the message includes the resolved county FIPS code
- the message includes the resolved NWS zone

Authentication:

- required

Request body:

Address lookup:

```json
{
  "address": {
    "line1": "123 Main St",
    "line2": "Suite 200",
    "city": "Gainesville",
    "stateCode": "FL",
    "postalCode": "32601"
  },
  "limit": 50,
  "offset": 0
}
```

Coordinate lookup:

```json
{
  "latitude": 29.6516,
  "longitude": -82.3248,
  "limit": 50,
  "offset": 0
}
```

Success response:

```json
{
  "location": {
    "matchedAddress": "123 MAIN ST, GAINESVILLE, FL, 32601",
    "latitude": 29.6516,
    "longitude": -82.3248,
    "countyFips": "FLC035",
    "nwsZone": "FLZ038"
  },
  "items": [
    {
      "id": 6731,
      "source": "NWWS",
      "eventCode": "TOR",
      "messageType": "Tornado Warning",
      "alertTypeCode": "tornado_warning",
      "title": "[ThunderCall] National Weather Wire Service Message",
      "status": "processed",
      "issuedAt": "2026-08-12T19:11:59Z",
      "receivedAt": "2026-08-12T19:11:59Z",
      "processedAt": "2026-08-12T19:12:00Z",
      "sourceMessageId": 7755,
      "externalMessageId": "smoke-message-20260812-1912",
      "sourceSegmentIndex": 0,
      "polygonWKT": null,
      "fipsCodes": ["FLC035"],
      "nwsZones": ["FLZ038"],
      "matchReasons": ["countyFips", "nwsZone"]
    }
  ],
  "page": {
    "total": 1,
    "limit": 50,
    "offset": 0
  }
}
```

Common errors:

- `400 Bad Request`: invalid JSON, invalid coordinates, or both address and coordinates provided
- `422 Unprocessable Entity`: the address or coordinates could not be resolved to a usable location
- `502 Bad Gateway`: external geocoding or NWS enrichment failed
- `500 Internal Server Error`: failed to load messages

Notes:

- `limit` defaults to `50`, max `200`
- `offset` defaults to `0`
- this endpoint searches across ingested ThunderCall messages by geography; it is not limited to messages that already have recipients in the current account

## `GET /v1/messages/{id}`

Returns one message plus its aggregated counts.

Authentication:

- required

Path parameters:

| Name | Type | Description |
| --- | --- | --- |
| `id` | integer | ThunderCall message ID |

Success response:

- same `Message` object described above

Common errors:

- `400 Bad Request`: message id must be a number
- `404 Not Found`: message not found
- `500 Internal Server Error`: failed to load message

## `POST /v1/users`

Creates a new account-scoped user, geocodes the supplied address, enriches it
with county FIPS and NWS zone, saves the location, and links the user to that
location.

Authentication:

- required

Request body:

```json
{
  "externalId": "student-1001",
  "firstName": "Pat",
  "lastName": "Smith",
  "displayName": "Pat Smith",
  "title": "Principal",
  "voicePhone": "+13525550123",
  "locationName": "Pat Smith Address",
  "subscriptionType": "address",
  "isPrimaryLocation": true,
  "address": {
    "line1": "123 Main St",
    "line2": "",
    "city": "Gainesville",
    "stateCode": "FL",
    "postalCode": "32601"
  }
}
```

Success response:

```json
{
  "user": {
    "id": 95199,
    "accountId": 8,
    "externalId": "student-1001",
    "firstName": "Pat",
    "lastName": "Smith",
    "displayName": "Pat Smith",
    "title": "Principal",
    "active": true
  },
  "location": {
    "id": 86420,
    "accountId": 8,
    "name": "Pat Smith Address",
    "addressLine1": "123 Main St",
    "addressLine2": null,
    "city": "Gainesville",
    "stateCode": "FL",
    "postalCode": "32601",
    "countyFips": "FLC035",
    "nwsZone": "FLZ038",
    "latitude": 29.6516,
    "longitude": -82.3248,
    "coverageWKT": "POINT (29.6516 -82.3248)",
    "isThunderCallEnabled": true,
    "active": true
  },
  "subscription": {
    "id": 17629,
    "userId": 95199,
    "locationId": 86420,
    "subscriptionType": "address",
    "isPrimary": true,
    "isThunderCallEnabled": true
  },
  "contactMethods": [
    {
      "id": 9001,
      "userId": 95199,
      "channel": "voice",
      "destination": "+13525550123",
      "isPrimary": true,
      "isVerified": false,
      "active": true
    }
  ],
  "resolved": {
    "matchedAddress": "123 MAIN ST, GAINESVILLE, FL, 32601",
    "latitude": 29.6516,
    "longitude": -82.3248,
    "countyFips": "FLC035",
    "nwsZone": "FLZ038"
  }
}
```

Common errors:

- `400 Bad Request`: missing display name / name fields, invalid address fields, or invalid subscription type
- `422 Unprocessable Entity`: the address could not be geocoded into both county FIPS and NWS zone
- `502 Bad Gateway`: external geocoding or NWS enrichment failed
- `500 Internal Server Error`: failed to create user or location

Notes:

- the created user is always attached to the authenticated account
- the saved location includes a `POINT` geometry in `coverage_geometry` so polygon-based matching works immediately
- `voicePhone` is optional, but if omitted the worker will not have a destination to call for this user

## `POST /api/users/signup`

Public compatibility endpoint for the existing station signup forms. This route
accepts the station signup payload, resolves the target account from
`accountId` in the request body, geocodes the submitted address, creates the
user and location, and stores per-alert voice preferences from the
`warningTypes` array.

Notes:

- authentication is not required
- the same handler is also available at `POST /api/products/{productId}/records` and `POST /v1/public/signups`
- responses from this endpoint use a top-level `message` field for compatibility with the legacy form
- the handler is rate-limited per client IP and returns `429 Too Many Requests`
  plus `Retry-After` when the configured limit is exceeded
- a true geocode no-match still returns `422 Unprocessable Entity`
- transient upstream resolver failures do not reject the signup; the API still
  creates the user and location, leaves geodata nullable, and returns
  `"enrichmentPending": true`

Request body:

```json
{
  "externalId": "RD1234567",
  "accountId": 2,
  "firstName": "Pat",
  "lastName": "Smith",
  "title": "",
  "tcall": true,
  "emails": [
    {
      "emailAddress": "pat@example.com",
      "emailType": "Home"
    }
  ],
  "phones": [
    {
      "phoneNumber": "4073530340",
      "extension": "",
      "phoneType": "Home"
    }
  ],
  "addresses": [
    {
      "address": "123 Main St",
      "address2": "",
      "city": "Tyler",
      "stateProvince": "TX",
      "zipPostalCode": "75701",
      "country": "US",
      "addressType": "Home",
      "thundercall": {
        "phoneSetting": {
          "name": "Home",
          "phoneType": "Home",
          "email": 0,
          "enableText": false
        },
        "warningTypes": [0, 2]
      }
    }
  ]
}
```

Notes:

- `accountId` is the new ThunderCall account id for the station receiving signups

Warning type mapping:

- `0` -> `tornado_warning`
- `1` -> `flash_flood_warning`
- `2` -> `severe_thunderstorm_warning`
- `3` -> `winter_storm_warning`
- `4` -> `tropical_storm`
- `5` -> `special_weather_statement`
- `6` -> `freeze_warning`

Success response:

```json
{
  "message": "Record created.",
  "enrichmentPending": false,
  "user": {
    "id": 95199,
    "accountId": 8,
    "externalId": "RD1234567",
    "firstName": "Pat",
    "lastName": "Smith",
    "displayName": "Pat Smith",
    "title": null,
    "active": true
  },
  "location": {
    "id": 86420,
    "accountId": 8,
    "name": "Pat Smith Address",
    "addressLine1": "123 Main St",
    "addressLine2": null,
    "city": "Tyler",
    "stateCode": "TX",
    "postalCode": "75701",
    "countyFips": "TXC423",
    "nwsZone": "TXZ149",
    "latitude": 32.3513,
    "longitude": -95.3011,
    "coverageWKT": "POINT (32.3513 -95.3011)",
    "isThunderCallEnabled": true,
    "active": true
  },
  "subscription": {
    "id": 17629,
    "userId": 95199,
    "locationId": 86420,
    "subscriptionType": "address",
    "isPrimary": true,
    "isThunderCallEnabled": true
  },
  "contactMethods": [
    {
      "id": 9001,
      "userId": 95199,
      "channel": "voice",
      "destination": "+14073530340",
      "isPrimary": true,
      "isVerified": false,
      "active": true
    },
    {
      "id": 9002,
      "userId": 95199,
      "channel": "email",
      "destination": "pat@example.com",
      "isPrimary": true,
      "isVerified": false,
      "active": true
    }
  ],
  "resolved": {
    "matchedAddress": "123 MAIN ST, TYLER, TX, 75701",
    "latitude": 32.3513,
    "longitude": -95.3011,
    "countyFips": "TXC423",
    "nwsZone": "TXZ149"
  }
}
```

If address geocoding or weather.gov enrichment is temporarily unavailable, the
same endpoint still returns `201 Created` with:

```json
{
  "message": "Record created; location enrichment pending.",
  "enrichmentPending": true
}
```

In that case the created location may have `null` `latitude`, `longitude`,
`countyFips`, `nwsZone`, and `coverageWKT` until a later enrichment pass fills
them in.

Common errors:

- `400 Bad Request`: missing required name, email, phone, address, or warning selection fields
- `404 Not Found`: `accountId` did not map to an active ThunderCall account
- `422 Unprocessable Entity`: the address could not be geocoded into both county FIPS and NWS zone
- `502 Bad Gateway`: external geocoding or NWS enrichment failed
- `500 Internal Server Error`: failed to create the new user or location

## `GET /v1/messages/{id}/locations`

Returns the impacted locations for a specific message, along with recipient counts.

Authentication:

- required

Path parameters:

| Name | Type | Description |
| --- | --- | --- |
| `id` | integer | ThunderCall message ID |

Success response:

```json
{
  "items": [
    {
      "id": 17,
      "name": "Fayetteville Campus",
      "addressLine1": "123 Main St",
      "city": "Fayetteville",
      "stateCode": "AR",
      "postalCode": "72701",
      "countyFips": "05143",
      "nwsZone": "ARZ103",
      "latitude": 36.0626,
      "longitude": -94.1574,
      "coverageWKT": "POLYGON((...))",
      "isThunderCallEnabled": true,
      "active": true,
      "subscribedUsersCount": 0,
      "matchedUsersCount": 44,
      "smsEnabledCount": 40,
      "emailEnabledCount": 18,
      "voiceEnabledCount": 12
    }
  ]
}
```

Common errors:

- `400 Bad Request`: message id must be a number
- `404 Not Found`: message not found
- `500 Internal Server Error`: failed to validate message access
- `500 Internal Server Error`: failed to load message locations

## `GET /v1/messages/{id}/deliveries`

Returns paginated recipient-level delivery results for a specific message.

Authentication:

- required

Path parameters:

| Name | Type | Description |
| --- | --- | --- |
| `id` | integer | ThunderCall message ID |

Query parameters:

| Name | Type | Description |
| --- | --- | --- |
| `search` | string | Case-insensitive search across recipient display name, first name, last name, and matched location name |
| `status` | string | Exact final `users_messages.status` filter |
| `limit` | integer | Page size, min `1`, max `200`, default `50` |
| `offset` | integer | Zero-based row offset, min `0`, default `0` |

Success response:

```json
{
  "items": [
    {
      "userMessageId": 901,
      "userId": 77,
      "displayName": "Pat Smith",
      "title": "Principal",
      "status": "sent",
      "queuedAt": "2026-07-31T14:00:07Z",
      "deliveredAt": "2026-07-31T14:00:14Z",
      "smsEnabled": true,
      "emailEnabled": true,
      "voiceEnabled": false,
      "matchedLocation": {
        "id": 17,
        "name": "Fayetteville Campus"
      },
      "attempts": [
        {
          "id": 1001,
          "channel": "sms",
          "destination": "+14795551212",
          "provider": "twilio",
          "providerMessageId": "SM123",
          "status": "sent",
          "errorMessage": null,
          "requestedAt": "2026-07-31T14:00:07Z",
          "sentAt": "2026-07-31T14:00:08Z",
          "deliveredAt": "2026-07-31T14:00:14Z"
        }
      ]
    }
  ],
  "page": {
    "total": 1,
    "limit": 50,
    "offset": 0
  }
}
```

Common errors:

- `400 Bad Request`: message id must be a number
- `400 Bad Request`: invalid query parameter
- `404 Not Found`: message not found
- `500 Internal Server Error`: failed to validate message access
- `500 Internal Server Error`: failed to load message deliveries

## `GET /v1/locations`

Returns paginated locations for the authenticated account.

Authentication:

- required

Query parameters:

| Name | Type | Description |
| --- | --- | --- |
| `search` | string | Case-insensitive search across location name, city, state, county FIPS, and NWS zone |
| `activeOnly` | boolean | Optional exact active filter |
| `limit` | integer | Page size, min `1`, max `200`, default `50` |
| `offset` | integer | Zero-based row offset, min `0`, default `0` |

Success response:

```json
{
  "items": [
    {
      "id": 17,
      "name": "Fayetteville Campus",
      "addressLine1": "123 Main St",
      "city": "Fayetteville",
      "stateCode": "AR",
      "postalCode": "72701",
      "countyFips": "05143",
      "nwsZone": "ARZ103",
      "latitude": 36.0626,
      "longitude": -94.1574,
      "coverageWKT": "POLYGON((...))",
      "isThunderCallEnabled": true,
      "active": true,
      "subscribedUsersCount": 44
    }
  ],
  "page": {
    "total": 1,
    "limit": 50,
    "offset": 0
  }
}
```

Common errors:

- `400 Bad Request`: invalid query parameter
- `500 Internal Server Error`: failed to load locations

## `GET /v1/locations/{id}`

Returns one location for the authenticated account.

Authentication:

- required

Path parameters:

| Name | Type | Description |
| --- | --- | --- |
| `id` | integer | ThunderCall location ID |

Success response:

- same `Location` object described above

Common errors:

- `400 Bad Request`: location id must be a number
- `404 Not Found`: location not found
- `500 Internal Server Error`: failed to load location

## Notes For Client Developers

- all authenticated routes are account-scoped
- message visibility is based on whether the account has recipient rows for the message
- `GET /v1/dashboard/summary` uses the same filters as `GET /v1/messages`
- `GET /v1/messages/{id}/locations` is the endpoint that best maps to the legacy UI’s “polygon with counts by location” panel
- `GET /v1/messages/{id}/deliveries` is the endpoint that best maps to the legacy UI’s recipient/results drill-down
