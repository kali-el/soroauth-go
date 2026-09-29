# Verification service

`server/` is a small HTTP service around `VerifyEntry`, for wallets and custody
systems that want a pre-submission check without embedding Go. It answers one
question per entry — do the signatures on it verify over the payload it commits
to? — with the same verdicts and the same JSON shape as
`soroauth verify --json`.

## Endpoints

`GET /healthz` reports `{"status":"ok","version":"..."}` for load balancers.

`POST /verify` takes a JSON body:

```json
{
  "entry": "<base64 authorization entry or transaction envelope>",
  "network": "testnet",
  "valid_until_ledger": 1234567
}
```

`network` accepts `testnet`, `futurenet`, `public`, or a literal passphrase,
resolved from the SDK's constants exactly like the CLI. `valid_until_ledger` is
optional; when present it is an assertion, not an input — the payload is always
rebuilt from the entry's own stored expiration, and a disagreement is an error.

The response for one entry is the CLI's report object; for an envelope it is an
array with `operation_index` and `entry_index` (one-based, as in the CLI). Each
credential node carries one of `verified`, `unsigned`, `invalid`, or
`cannot_check`, with a `reason` when there is something to explain. See
`verify --help` for what each verdict means and what verification cannot
establish offline.

A verification that completes is HTTP 200 whatever the verdicts say: a body
reporting `invalid` is the answer, not a transport failure. That maps to the
CLI's exit code 4, which likewise fires after the report is printed. Only a
request the service cannot verify is an error with an `{"error"}` body and no
report:

| Status | Meaning                                                             |
| ------ | ------------------------------------------------------------------- |
| 200    | Verification completed; read the verdicts.                          |
| 400    | Malformed JSON, missing field, undecodable entry, or a contradicted |
|        | `valid_until_ledger` assertion.                                     |
| 405    | Wrong method (`/verify` is POST-only, `/healthz` is GET-only).      |
| 413    | Body past the 1 MB cap.                                             |

Unlike the CLI there is no `--allow-unsigned` switch: the service reports every
verdict and the caller applies its own policy to them.

## What it is not

The service is stateless and holds no keys. Verification needs public data only
— the entry, the network passphrase, an optional expiration assertion — so there
is no field that accepts a secret, nothing is stored, and request bodies are
never logged. Do not send it secrets; it has nowhere to put them.

Rate limiting, authentication, and TLS are the operator's concern. Run it behind
a reverse proxy that provides them; this service deliberately invents no auth
scheme of its own. Request bodies are capped at 1 MB and server timeouts
(`READ_TIMEOUT`, `WRITE_TIMEOUT`, `IDLE_TIMEOUT`) default to 5s/10s/120s, but
those are resource bounds, not access control.

## Running it

```sh
go run ./server                  # listens on :8080
ADDR=127.0.0.1:8080 go run ./server
```

`make server` builds `bin/soroauth-server` with the release version stamped in
(reported by `/healthz` and `soroauth-server version`). The published container
image is CLI-only; serving this over a network is a deployment choice the image
does not make for you.

## Coverage

`server/main_test.go` serves the real routes over `httptest` and covers a signed
entry (verified), a partially signed delegates tree (one verified node, the rest
unsigned), malformed requests (missing fields, non-base64, base64-but-not-XDR,
non-JSON, wrong method), a tampered expiration (invalid), a contradicted
`valid_until_ledger` assertion, and an envelope input with its one-based
positions. The verdicts are asserted against the library's `Verdict*` constants
rather than string literals, so the tests fail if the service ever stops
speaking the CLI's verdicts.
