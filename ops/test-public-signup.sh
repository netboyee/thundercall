#!/usr/bin/env bash
set -euo pipefail

usage() {
  cat <<'EOF'
Usage:
  ops/test-public-signup.sh [options]

Required:
  --account-id ID
  --first-name NAME
  --last-name NAME
  --email ADDRESS
  --phone NUMBER
  --line1 ADDRESS
  --city CITY
  --state STATE
  --postal-code ZIP

Authentication:
  Set SIGNUP_SHARED_SECRET or THUNDERCALL_API_PUBLIC_SIGNUP_PROXY_SHARED_SECRET,
  or pass --shared-secret SECRET.

Optional:
  --title TITLE
  --line2 ADDRESS
  --warning-types CSV          Default: 0,2
  --external-id ID             Default: CURL-TEST-<unix timestamp>
  --base-url URL               Default: https://api.thundercall.com
  --client-ip IP               Default: 203.0.113.10
  --show-payload               Print the signed request body before sending
  --help

Example:
  SIGNUP_SHARED_SECRET='...' \
  ops/test-public-signup.sh \
    --account-id 4 \
    --first-name Pat \
    --last-name Smith \
    --email pat.smith@example.com \
    --phone 4073530340 \
    --line1 "600 Austin Ave" \
    --city Waco \
    --state TX \
    --postal-code 76701 \
    --warning-types 0,2
EOF
}

require_command() {
  if ! command -v "$1" >/dev/null 2>&1; then
    printf 'Missing required command: %s\n' "$1" >&2
    exit 1
  fi
}

account_id=""
first_name=""
last_name=""
title=""
email_address=""
phone_number=""
line1=""
line2=""
city=""
state_code=""
postal_code=""
warning_types_csv="0,2"
external_id="CURL-TEST-$(date +%s)"
base_url="https://api.thundercall.com"
client_ip="203.0.113.10"
shared_secret="${SIGNUP_SHARED_SECRET:-${THUNDERCALL_API_PUBLIC_SIGNUP_PROXY_SHARED_SECRET:-}}"
show_payload="false"

while [[ $# -gt 0 ]]; do
  case "$1" in
    --account-id)
      account_id="${2:-}"
      shift 2
      ;;
    --first-name)
      first_name="${2:-}"
      shift 2
      ;;
    --last-name)
      last_name="${2:-}"
      shift 2
      ;;
    --title)
      title="${2:-}"
      shift 2
      ;;
    --email)
      email_address="${2:-}"
      shift 2
      ;;
    --phone)
      phone_number="${2:-}"
      shift 2
      ;;
    --line1)
      line1="${2:-}"
      shift 2
      ;;
    --line2)
      line2="${2:-}"
      shift 2
      ;;
    --city)
      city="${2:-}"
      shift 2
      ;;
    --state)
      state_code="${2:-}"
      shift 2
      ;;
    --postal-code)
      postal_code="${2:-}"
      shift 2
      ;;
    --warning-types)
      warning_types_csv="${2:-}"
      shift 2
      ;;
    --external-id)
      external_id="${2:-}"
      shift 2
      ;;
    --base-url)
      base_url="${2:-}"
      shift 2
      ;;
    --client-ip)
      client_ip="${2:-}"
      shift 2
      ;;
    --shared-secret)
      shared_secret="${2:-}"
      shift 2
      ;;
    --show-payload)
      show_payload="true"
      shift
      ;;
    --help|-h)
      usage
      exit 0
      ;;
    *)
      printf 'Unknown option: %s\n\n' "$1" >&2
      usage >&2
      exit 1
      ;;
  esac
done

for value_name in \
  account_id \
  first_name \
  last_name \
  email_address \
  phone_number \
  line1 \
  city \
  state_code \
  postal_code
do
  if [[ -z "${!value_name}" ]]; then
    printf 'Missing required value: %s\n\n' "$value_name" >&2
    usage >&2
    exit 1
  fi
done

if [[ -z "$shared_secret" ]]; then
  printf 'Missing shared secret. Set SIGNUP_SHARED_SECRET, THUNDERCALL_API_PUBLIC_SIGNUP_PROXY_SHARED_SECRET, or pass --shared-secret.\n' >&2
  exit 1
fi

require_command curl
require_command openssl
require_command python3
require_command xxd

body_file="$(mktemp /tmp/thundercall-signup-body.XXXXXX.json)"
trap 'rm -f "$body_file"' EXIT

ACCOUNT_ID="$account_id" \
FIRST_NAME="$first_name" \
LAST_NAME="$last_name" \
TITLE="$title" \
EMAIL_ADDRESS="$email_address" \
PHONE_NUMBER="$phone_number" \
LINE1="$line1" \
LINE2="$line2" \
CITY="$city" \
STATE_CODE="$state_code" \
POSTAL_CODE="$postal_code" \
WARNING_TYPES_CSV="$warning_types_csv" \
EXTERNAL_ID="$external_id" \
python3 >"$body_file" <<'PY'
import json
import os
import sys

warning_csv = os.environ["WARNING_TYPES_CSV"].strip()
if not warning_csv:
    warning_types = []
else:
    try:
        warning_types = [int(part.strip()) for part in warning_csv.split(",") if part.strip()]
    except ValueError as exc:
        raise SystemExit(f"Invalid warning type list: {exc}")

payload = {
    "externalId": os.environ["EXTERNAL_ID"],
    "accountId": int(os.environ["ACCOUNT_ID"]),
    "firstName": os.environ["FIRST_NAME"],
    "lastName": os.environ["LAST_NAME"],
    "title": os.environ["TITLE"],
    "emailAddress": os.environ["EMAIL_ADDRESS"],
    "phoneNumber": os.environ["PHONE_NUMBER"],
    "address": {
        "line1": os.environ["LINE1"],
        "line2": os.environ["LINE2"],
        "city": os.environ["CITY"],
        "stateCode": os.environ["STATE_CODE"],
        "postalCode": os.environ["POSTAL_CODE"],
    },
    "warningTypes": warning_types,
}

json.dump(payload, sys.stdout, separators=(",", ":"))
PY

timestamp="$(date -u +"%Y-%m-%dT%H:%M:%SZ")"
signature="$(
  {
    printf 'POST\n/api/users/signup\n%s\n' "$timestamp"
    cat "$body_file"
  } | openssl dgst -sha256 -hmac "$shared_secret" -binary | xxd -p -c 256
)"

printf 'POST %s/api/users/signup\n' "${base_url%/}"
printf 'X-Thundercall-Signup-Timestamp: %s\n' "$timestamp"
printf 'X-Thundercall-Client-IP: %s\n' "$client_ip"
printf 'External ID: %s\n' "$external_id"

if [[ "$show_payload" == "true" ]]; then
  printf '\nPayload:\n'
  python3 -m json.tool "$body_file"
  printf '\n'
fi

curl -i --silent --show-error \
  "${base_url%/}/api/users/signup" \
  -H 'Accept: application/json' \
  -H 'Content-Type: application/json' \
  -H "X-Thundercall-Signup-Timestamp: $timestamp" \
  -H "X-Thundercall-Signup-Signature: $signature" \
  -H "X-Thundercall-Client-IP: $client_ip" \
  --data-binary "@$body_file"
