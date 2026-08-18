# NWWS Messages

This document explains the National Weather Wire Service (NWWS) messages that ThunderCall ingests, how those messages are formatted, and how the Go code parses them.

ThunderCall currently connects to the NWWS Open Interface XMPP room and consumes weather/public safety bulletins such as:

- `SVR` severe thunderstorm warnings
- `FFW` flash flood warnings
- `TOR` tornado warnings
- `WSW` winter storm warnings

The parser and normalizer live primarily in:

- `internal/nwws/parser.go`
- `internal/nwws/types.go`
- `internal/nwws/normalize.go`
- `internal/ingest/service.go`

## High-Level Flow

1. NWWS sends an XMPP groupchat message.
2. ThunderCall reads the NWWS-specific XML extension and extracts the raw bulletin text.
3. The raw bulletin is parsed into structured parts such as WMO header, AWIPS ID, UGC codes, VTEC, and polygon.
4. The parsed message is normalized into one or more ThunderCall message records.
5. Only configured/supported products are persisted and forwarded into the rest of the pipeline.

## 1. NWWS Transport Format

NWWS messages arrive as XMPP groupchat stanzas. Conceptually they look like this:

```xml
<message type="groupchat">
  <body>Human-readable preview text</body>
  <x cccc="KJKL"
     ttaaii="WUUS53"
     issue="2026-08-11T17:45:00Z"
     awipsid="SVRJKL"
     id="nwws_processor.12345">
    <![CDATA[
    1234
    WUUS53 KJKL 111745
    SVRJKL
    ...
    ]]>
  </x>
</message>
```

ThunderCall primarily uses the NWWS extension payload, not the preview text in `<body>`.

The extension attributes map to `internal/nwws.StanzaEnvelope` like this:

| NWWS field | ThunderCall field | Meaning |
| --- | --- | --- |
| `cccc` | `CCCCode` | 4-character issuing center / office code |
| `ttaaii` | `WMOCode` | WMO product identifier |
| `issue` | `IssueTime` | RFC3339 UTC timestamp from the NWWS transport |
| `awipsid` | `AWIPSID` | AWIPS/AFOS product identifier such as `SVRJKL` |
| `id` | `ExternalID` | Unique NWWS message identifier used for dedupe |
| payload text | `Body` | Full raw bulletin text |

Notes:

- NWWS often prefixes the raw payload with a byte-count line. ThunderCall strips that before parsing.
- On room join, NWWS may replay recent room history. Those replayed stanzas are not always equivalent to a fresh live message.

## 2. Raw Bulletin Layout

Once the NWWS payload is extracted, the bulletin text usually looks like this:

```text
WUUS53 KDMX 152118
SVRDMX
IAC049-153-181-152200-
/O.NEW.KDMX.SV.W.0123.260815T2118Z-260815T2200Z/

BULLETIN - IMMEDIATE BROADCAST REQUESTED
Severe Thunderstorm Warning
National Weather Service Des Moines IA
418 PM CDT Sat Aug 15 2026

The National Weather Service in Des Moines has issued a
...
LAT...LON 4153 9421 4164 9382 4137 9369 4126 9406
...
$$
```

The major sections are:

1. WMO header line
2. AWIPS product identifier line
3. UGC geography line
4. One or more VTEC lines
5. MND/product header text
6. Free-form bulletin body
7. Optional `LAT...LON` polygon section
8. Footer, usually after `$$`

## 3. Parsed Fields

### WMO Header

Example:

```text
WUUS53 KJKL 111745
```

ThunderCall parses this into `WMOHeader`:

- `DataType`: `WUUS53`
- `IssuingOffice`: `KJKL`
- `IssuedAt`: parsed from `111745` as day/hour/minute in UTC
- `BBBDesignator`: optional fourth token if present

Important:

- The WMO header time is inside the bulletin body.
- The NWWS transport also has its own `issue` timestamp.
- ThunderCall keeps both. The source-envelope timestamp and the bulletin-issued timestamp are related but not identical fields.

### AWIPS Identifier

Example:

```text
SVRJKL
```

ThunderCall parses this into `AWIPSIdentifier`:

- `ProductCategory`: `SVR`
- `OriginatingOffice`: `JKL`

In practice, the first 3 characters usually tell you the product family:

- `SVR` severe thunderstorm warning
- `TOR` tornado warning
- `FFW` flash flood warning
- `WSW` winter storm warning

### MND / Product Header

The text block after the UGC/VTEC lines becomes `MNDHeader`. ThunderCall captures:

- `BroadcastInstruction`
  Example: `BULLETIN - IMMEDIATE BROADCAST REQUESTED`
- `ProductName`
  Example: `Severe Thunderstorm Warning`
- `IssuingOffice`
  Example: `National Weather Service Des Moines IA`
- `IssuanceDateTime`
  Example: `418 PM CDT Sat Aug 15 2026`

### UGC Codes

Example:

```text
KYC159-195-111815-
```

ThunderCall parses this into one or more `UGCCode` values:

- `State`: `KY`
- `Format`: `C` or `Z`
- `Code`: `159`, `195`, etc.
- `ExpiresAt`: parsed from the trailing `DDHHMM`

Meaning of `Format`:

- `C` means county/parish-style UGC codes
- `Z` means NWS forecast zones

ThunderCall normalizes these into:

- `FIPSCodes` for `C` entries, such as `KYC159`
- `NWSZones` for `Z` entries, such as `FLZ041`

Notes:

- UGC lines can contain ranges, such as `001>005`.
- `ALL` and `000` are expanded by the parser as full ranges.

### Primary VTEC

Example:

```text
/O.NEW.KJKL.SV.W.0095.260811T1745Z-260811T1815Z/
```

ThunderCall parses this into `PrimaryVTEC`:

- `ProductClass`: `O`
- `Action`: `NEW`
- `OfficeID`: `KJKL`
- `Phenomenon`: `SV`
- `Significance`: `W`
- `ETN`: `0095`
- `BeginsAtRaw`: `260811T1745Z`
- `EndsAtRaw`: `260811T1815Z`
- `BeginsAt`: parsed UTC timestamp
- `EndsAt`: parsed UTC timestamp

Common `Action` values you will see:

- `NEW`: new alert
- `CON`: continued alert
- `EXT`: extended alert
- `EXA` / `EXB`: expanded or adjusted alert area
- `CAN`: canceled alert
- `EXP`: expired alert
- `COR`: corrected alert

Important:

- ThunderCall preserves the AWIPS product family, such as `SVR`, and also stores the parsed VTEC details separately.
- A single bulletin can contain more than one VTEC line.

### Hydrologic VTEC

Some flood-oriented products include a second VTEC line immediately after the primary one. ThunderCall parses that into `HydrologicVTEC` fields such as:

- `NWSLocationIdentifier`
- `FloodSeverity`
- `ImmediateCause`
- `BeginsAt`
- `CrestAt`
- `EndsAt`
- `FloodRecord`

### Segment Text

The main bulletin body is kept in:

- `Segment.Message`
- `ParsedMessage.ProductHeadlineOverview`
- `ParsedMessage.Footer`

ThunderCall later combines the relevant parts into the normalized message body stored in the app.

## 4. Segments

NWWS bulletins are not always single-segment messages.

A parsed `ParsedMessage` contains:

- one top-level WMO header
- one top-level AWIPS identifier
- zero or more `Segments`

Each segment can have its own:

- UGC codes
- VTEC
- plain-language geography text
- city list
- polygon
- message body

ThunderCall normalizes each accepted segment into its own downstream message record. That is why one NWWS bulletin can produce multiple ThunderCall messages.

## 5. Polygon Parsing

ThunderCall looks for a `LAT...LON` section inside the segment body.

Example:

```text
LAT...LON 4153 9421 4164 9382 4137 9369 4126 9406
```

Parsing rules in this codebase:

1. Read consecutive numeric tokens after `LAT...LON`
2. Treat each pair as one coordinate
3. Convert `4153` to `41.53`
4. Convert `9421` to `-94.21`
5. Stop when a non-numeric line is reached

The parser first stores points as:

- `Coordinate.Latitude`
- `Coordinate.Longitude`

ThunderCall then converts them to WKT for storage and matching.

Important ThunderCall behavior:

- If the polygon is not already closed, ThunderCall appends the first point to the end before serializing it.
- The current codebase writes WKT points as `latitude longitude`, for example `POLYGON ((41.53 -94.21,...))`.
- That ordering is an internal ThunderCall convention used consistently in this codebase; it is not the usual GIS `longitude latitude` convention.

## 6. What Gets Persisted

Not every stanza seen in the NWWS room becomes a database row.

Current behavior:

- ThunderCall parses the bulletin
- it filters to configured/supported products
- it drops segments that are not loadable by ThunderCall
- it persists accepted rows into `source_messages` and `messages`

That means:

- `source_messages` is not a full archive of every XMPP stanza in the room
- unsupported products are ignored before persistence
- a quiet `source_messages` table does not always mean the room itself was quiet

At the time of writing, the default configured product set is:

- `SVR`
- `FFW`
- `TOR`
- `WSW`

## 7. ThunderCall Field Mapping Summary

At a high level, the normalized ThunderCall message pulls from NWWS like this:

| ThunderCall concept | NWWS source |
| --- | --- |
| source message external ID | NWWS stanza `id` |
| source message issue time | NWWS stanza `issue` |
| event/product code | AWIPS product category, such as `SVR` |
| alert type / title | derived from product family and bulletin text |
| message body | MND/product header plus segment body/footer |
| polygon | parsed from `LAT...LON` |
| county matches | parsed from `C` UGC entries |
| zone matches | parsed from `Z` UGC entries |
| VTEC action and timing | parsed from primary VTEC |

## 8. Troubleshooting Notes

If you are debugging ingestion:

- Check whether the XMPP room connection is healthy first.
- Remember that unsupported products are intentionally ignored.
- If you need to prove that raw room traffic is arriving, temporarily enable ingest debug logging so raw NWWS payload metadata is emitted to the logs.
- If a product has multiple segments, one stanza can legitimately create multiple ThunderCall message rows.

## 9. Useful Code References

- `internal/nwws/parser.go`
  Low-level NWWS parsing
- `internal/nwws/helpers.go`
  Timestamp helpers and WKT polygon generation
- `internal/nwws/normalize.go`
  Converts parsed NWWS data into ThunderCall requests
- `internal/ingest/service.go`
  Applies product filtering, dedupe, persistence, and outbox creation
