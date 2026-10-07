# Piranid Authentication Service

**Status:** implemented
**Scope:** OAuth 2.0 authorization server (authorization code + PKCE), RS256 JWT issuance, JWKS publication, and bearer-token verification by consuming services.

---

## Table of contents

1. [What the service does](#1-what-the-service-does)
2. [Architecture](#2-architecture)
3. [Request lifecycle](#3-request-lifecycle)
4. [Token lifecycle](#4-token-lifecycle)
5. [Endpoints](#5-endpoints)
6. [Files and descriptors](#6-files-and-descriptors)
7. [Data model](#7-data-model)
8. [Configuration](#8-configuration)
9. [Security properties](#9-security-properties)
10. [Verification](#10-verification)
11. [Deferred work](#11-deferred-work)

---

## 1. What the service does

The Auth node is the only component in Piranid that holds a credential secret.
It performs two jobs:

1. **Authorization server** — authenticates a user, then issues short-lived
   access tokens to registered services.
2. **Token verification provider** — publishes its public signing key so that
   services can verify tokens without ever holding the signing key.

The second half is what makes the first safe to scale. A service that can verify
a token but cannot sign one gains nothing from being compromised.

### Grant and token types

| Capability                                          | Status                              |
| --------------------------------------------------- | ----------------------------------- |
| Authorization code grant (`authorization_code`)     | Implemented                         |
| PKCE with `S256` (mandatory)                        | Implemented                         |
| RS256 JWT access tokens                             | Implemented                         |
| JWKS publication (`/.well-known/jwks.json`)         | Implemented                         |
| Signature / issuer / audience / expiry verification | Implemented                         |
| Bearer middleware for HTTP services                 | Implemented, wired into Event_Queue |
| Refresh token grant                                 | Deferred                            |
| Implicit grant                                      | Not implemented (deliberate)        |
| Client credentials grant                            | Deferred                            |
| OpenID Connect (ID tokens, `/userinfo`, discovery)  | Deferred                            |

---

## 2. Architecture

```
                    ┌──────────────────────────────────────┐
                    │        Auth Node  (:8081)            │
   user agent ────▶ │                                      │
   (browser)        │  GET  /.well-known/jwks.json         │
        │           │         │                            │
        │           │  GET  /authorize      ──▶ consent    │
        │           │  POST /authorize/consent             │
        │           │         │ issues opaque code          │
        │           │         ▼                            │
        │           │  POST /token            ◀──┐         │
        │           │         │                  │         │
        │           │         ▼                  │         │
        │           │  SQLite (clients, codes)   │         │
        │           │  RSA private key (signing) │         │
        │           └───────────┬──────────────────┘         │
        │                       │                            │
        │                   RS256 JWT                         │
        │                       │                            │
        │                       ▼                            │
        │           ┌──────────────────────────────────────┐│
        │           │  Consumer services                   ││
        │           │  (Event_Queue, eventually others)    ││
        │           │                                      ││
        │           │  fetch JWKS ──▶ verify sig/iss/aud/exp│
        │           └──────────────────────────────────────┘│
        │                                                    │
        └──────────────── 302 redirect with code ───────────▶│ Client app
                                                               │
                                     POST /token (back channel) ┘
```

**Trust boundary.** The private key never leaves the Auth node. Consumers hold
only the public key, fetched from the JWKS endpoint. A consumer compromise yields
the ability to _verify_ tokens, not to _mint_ them.

### Why the flow is split across three endpoints

| Endpoint               | Channel                        | Carries                        |
| ---------------------- | ------------------------------ | ------------------------------ |
| `/authorize` + consent | Browser (front channel)        | The user's password            |
| `/token`               | Client → server (back channel) | Client secret, `code_verifier` |

The password travels only to the Auth node, never to the client application.
The client secret and the PKCE verifier travel only over the back channel, where
a browser redirect cannot observe them.

---

## 3. Request lifecycle

### 3.1 Registration (one-time, per client)

```
Client app                    Auth node                    SQLite
    │                             │                           │
    │ POST /register              │                           │
    │  client_name                │                           │
    │  redirect_uri               │                           │
    │  username, password         │                           │
    ├────────────────────────────▶│                           │
    │                             │ validate redirect_uri     │
    │                             │ is absolute http(s)      │
    │                             │                           │
    │                             │ bcrypt(password, cost 12)│
    │                             │ generate client_id (128b) │
    │                             │ generate secret (256b)    │
    │                             │ bcrypt(secret)            │
    │                             ├──────────────────────────▶│
    │                             │      INSERT credentials   │
    │ 201 Created                 │                           │
    │  client_id, client_secret   │                           │
    │◀────────────────────────────┤                           │
```

The plaintext `client_secret` is returned **exactly once**. Only its bcrypt hash
is stored, so it is unrecoverable afterwards. Losing it means re-registering.

Rejected: missing fields, a non-absolute `redirect_uri`, a password over 72 bytes
(bcrypt truncates silently), and duplicate `username`/`client_id`.

### 3.2 Authorization code + PKCE (per session)

```
Client app        User agent        Auth node               SQLite
    │                 │                │                      │
    │ ① redirect user │                │                      │
    │   to /authorize │                │                      │
    │   + query params│                │                      │
    ├────────────────▶│                │                      │
    │                 │ GET /authorize │                      │
    │                 │  response_type=code                    │
    │                 │  client_id      │                      │
    │                 │  redirect_uri   │                      │
    │                 │  scope, state   │                      │
    │                 │  nonce          │                      │
    │                 │  code_challenge │                      │
    │                 │  code_challenge_method=S256            │
    │                 ├───────────────▶│                      │
    │                 │                │ ② validate:         │
    │                 │                │  load client         │
    │                 │                │  redirect_uri must   │
    │                 │                │   match EXACTLY      │
    │                 │                │  response_type=code  │
    │                 │                │  PKCE S256 required  │
    │                 │                │                      │
    │                 │ ③ render consent page (password field) │
    │                 │◀───────────────┤                      │
    │                 │                │                      │
    │ ④ user types password, clicks Allow                    │
    │                 │ POST /authorize/consent                │
    │                 │  (all params echoed as hidden fields)  │
    │                 ├───────────────▶│                      │
    │                 │                │ ⑤ revalidate EVERYTHING│
    │                 │                │   (form is untrusted) │
    │                 │                │ bcrypt.compare(pw)    │
    │                 │                │ confirm user owns     │
    │                 │                │   this client         │
    │                 │                │                      │
    │                 │                │ generate code (256b) │
    │                 │                ├─────────────────────▶│
    │                 │                │  INSERT auth_codes   │
    │                 │                │  (code + PKCE binding)│
    │                 │ 302 Location:  │                      │
    │                 │  redirect_uri? │                      │
    │                 │  code=…&state= │                      │
    │◀────────────────┼────────────────┤                      │
    │ ⑦ exchange code │                │                      │
    │ POST /token     │                │                      │
    │  grant_type     │                │                      │
    │  code           │                │                      │
    │  redirect_uri   │                │                      │
    │  client_id      │                │                      │
    │  client_secret  │                │                      │
    │  code_verifier  │                │                      │
    ├─────────────────┼───────────────▶│                      │
    │                 │                │ ⑥ authenticate client│
    │                 │                │ ⑦ ATOMIC consume:    │
    │                 │                │   SELECT + DELETE    │
    │                 │                │   in one tx          │
    │                 │                ├─────────────────────▶│
    │                 │                │ ⑧ re-check binding:  │
    │                 │                │   client_id match    │
    │                 │                │   redirect_uri match │
    │                 │                │   not expired        │
    │                 │                │   PKCE: S256(verify) │
    │                 │                │   == challenge       │
    │                 │                │                      │
    │                 │                │ ⑨ RS256 sign JWT     │
    │ 200 OK          │                │                      │
    │  access_token   │                │                      │
    │  token_type     │                │                      │
    │  expires_in     │                │                      │
    │◀────────────────┤                │                      │
```

### 3.3 Requesting a protected resource

```
Client app         Consumer service                Auth node
    │                     │                            │
    │ GET /services/x     │                            │
    │ Authorization:      │                            │
    │   Bearer <JWT>      │                            │
    ├────────────────────▶│                            │
    │                     │ ① extract Bearer token     │
    │                     │ ② resolve key by kid       │
    │                     │    (cached JWKS)           │
    │                     │ ③ alg MUST be RS256        │
    │                     │ ④ verify signature         │
    │                     │ ⑤ iss == expected          │
    │                     │ ⑥ aud == expected          │
    │                     │ ⑦ exp > now (with leeway)  │
    │                     │    nbf <= now              │
    │                     │ ⑧ token_use == access      │
    │ 200 OK              │                            │
    │◀────────────────────┤                            │
```

If step ② meets an unknown `kid`, the service refetches the JWKS (throttled to
once per 5 minutes minimum). This is how a rotated signing key propagates without
restarting every consumer.

### 3.4 Error reporting rule

The distinction that governs the whole authorize path: **once `redirect_uri` is
validated, errors are returned to the client; before that, they are rendered
directly.**

| Situation                                             | Response                                                                  |
| ----------------------------------------------------- | ------------------------------------------------------------------------- |
| Unknown `client_id`, or `redirect_uri` not registered | Rendered locally. Redirecting would make the endpoint an open redirector. |
| Any error after `redirect_uri` is validated           | 302 to `redirect_uri?error=…&state=…`                                     |
| Error at `/token`                                     | JSON body, HTTP 400/401                                                   |

---

## 4. Token lifecycle

### Access token

A stateless, self-contained RS256 JWT.

```
Header   { "alg": "RS256", "kid": "<RFC 7638 thumbprint>", "typ": "JWT" }
Payload  {
           "iss":  "https://auth.piranid.local",   ← deployment identity
           "aud":  "event-queue",                   ← intended service
           "sub":  "alice",                         ← authenticated user
           "exp":  1791405629,                      ← expiry, checked
           "iat":  1791404729,
           "nbf":  1791404729,                      ← not-before, checked
           "jti":  "k5u4K_cj-ZOwUMcfXpzrOw",        ← unique id
           "scope": "events:read",
           "client_id": "AaWzNni2ycum2A2xORXLYg",
           "token_use": "access"                    ← kind discrimination
         }
Signature  256 bytes, RSA PKCS#1 v1.5 over SHA-256
```

**Default TTL: 15 minutes.** Not verifiable-after-the-fact, so it is short.

`iss` must be stable across restarts. It is **not** derived from the node's
`Service_ID`, which is a fresh UUID every boot — using it would invalidate every
outstanding token on each restart.

### Authorization code

An opaque 256-bit random string, **not** a JWT.

Opaque because the server must be able to reject an already-redeemed code. A
self-contained token cannot be revoked before it expires. **Default TTL: 2
minutes.**

Stored with its full binding, all of which is re-checked at redemption:

| Column                    | Purpose                         |
| ------------------------- | ------------------------------- |
| `code`                    | The opaque value (primary key)  |
| `client_id`               | Must match the redeeming client |
| `subject`                 | The authenticated user          |
| `scope`                   | Granted scopes                  |
| `redirect_uri`            | Must match the token request    |
| `code_challenge`          | S256 challenge                  |
| `code_challenge_method`   | Always `S256`                   |
| `state`                   | Echoed to the client            |
| `nonce`                   | Echoed to the client            |
| `issued_at`, `expires_at` | Lifetime                        |

### Expiry handling

| Artifact           | Enforcement                                       |
| ------------------ | ------------------------------------------------- |
| Authorization code | Server-side check at `/token`; row deleted on use |
| Access token       | `exp` checked by every consumer                   |

---

## 5. Endpoints

All paths are at the server root. Methods are declared, so a mismatch returns
405 rather than reaching the handler.

### `GET /.well-known/jwks.json`

Public signing keys. No authentication — it must be reachable by services that
have no credential yet.

```json
{
  "keys": [
    {
      "kty": "RSA",
      "use": "sig",
      "alg": "RS256",
      "kid": "uVO92HL3E17imBv4qbaDpOVlRV8MH1a6upqR2f9MluQ",
      "n": "4AD68VZsU29PBmg8...",
      "e": "AQAB"
    }
  ]
}
```

Only public material is ever serialized; the `JWK` struct has no field for a
private exponent. `Cache-Control: public, max-age=300`.

### `GET /authorize`

Renders the consent page. Query parameters: `response_type`, `client_id`,
`redirect_uri`, `scope`, `state`, `nonce`, `code_challenge`,
`code_challenge_method`.

Rejects: `response_type != code`, unregistered `redirect_uri`, missing or
non-S256 `code_challenge`.

### `POST /authorize/consent`

`application/x-www-form-urlencoded`. Authenticates the user, issues a code, 302s
to `redirect_uri?code=…&state=…`.

Every parameter from the original query string is echoed into hidden form fields
and **re-validated server-side** — a tampered form cannot redirect elsewhere or
skip PKCE.

`approve=approve` grants; any other value denies with `error=access_denied`.

### `POST /token`

`grant_type=authorization_code` only. Accepts form-encoded or JSON bodies.
Client credentials may be in the body or an HTTP Basic header.

```json
{
  "access_token": "eyJhbGciOiJSUzI1NiIs...",
  "token_type": "Bearer",
  "expires_in": 900,
  "scope": "events:read"
}
```

### `POST /register`

JSON or form-encoded. Returns 201 with the one-time plaintext `client_secret`.

### `GET /healthz`

`{"status":"ok"}`. Unauthenticated and dependency-free, so Kubernetes can probe
it without a credential. Registered on Auth and Event_Queue.

### `POST /logout`

Explicit no-op. Access tokens are stateless and self-expiring; there is no
server-side session to destroy. Revocation would need a deny-list (reintroducing
shared state) or a refresh-token store. It says so rather than pretending.

### Error codes (RFC 6749 §5.2)

`invalid_request`, `invalid_client`, `invalid_grant`, `unauthorized_client`,
`unsupported_grant_type`, `invalid_scope`, `access_denied`,
`unsupported_response_type`, `server_error`, `temporarily_unavailable`.

---

## 6. Files and descriptors

### `nodes/Auth` — the authorization server

| File                            | Lines | Role                                                                                                                                                                     |
| ------------------------------- | ----: | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| `main.go`                       |   139 | Entrypoint. Validates config, opens SQLite via `dbutil`, loads the RSA key, wires telemetry and logging, runs the server, drains on SIGINT/SIGTERM.                      |
| `authcore/auth_core.go`         |   211 | `AuthNode`. Builds the data managers, loads config and key, creates the signer, parses templates, registers all routes. Refuses to start on bad config or a missing key. |
| `handlers/routes.go`            |   782 | All HTTP handlers: JWKS, authorize, consent, token, register, logout. Owns validation ordering and the redirect-vs-render decision.                                      |
| `transactions/oauth.go`         |   155 | Wire types. `Error` implements RFC 6749 §5.2 bodies; `AuthorizeRequest`, `ConsentForm`, `TokenRequest`, `TokenResponse`, `RegisterRequest`.                              |
| `models/auth_entry.go`          |    88 | `AuthEntry`. A registered client plus its user. `AllowsRedirect` does exact-match against the registered set.                                                            |
| `models/auth_code_entry.go`     |    66 | `AuthCodeEntry` plus `AuthCodeColumns`, the exact column list the scanner reads.                                                                                         |
| `utils/pkce.go`                 |   104 | `S256Challenge`, `VerifyPKCE` (constant-time), `ValidateCodeChallenge`, `GenerateCodeVerifier`.                                                                          |
| `utils/password.go`             |    79 | bcrypt at cost 12. `HashPassword`, `CheckPassword`, `HashSecret`, `CheckSecret`. `ErrInvalidCredentials` keeps failure reasons indistinguishable.                        |
| `utils/random.go`               |    43 | `GenerateAuthCode` (256-bit), `GenerateClientID` (128-bit), `GenerateSecret` (256-bit), all from `crypto/rand`.                                                          |
| `utils/auth_code_inserter.go`   |    40 | `INSERT` into `auth_codes` with every column.                                                                                                                            |
| `utils/auth_code_deleter.go`    |    50 | `DELETE` by `code`; passed to `ConsumeEntry` so delete shares the read's transaction. `AuthCodeSweeper` for expiry cleanup.                                              |
| `utils/auth_code_scanner.go`    |    36 | Reads a code row in `AuthCodeColumns` order.                                                                                                                             |
| `utils/credentials_inserter.go` |    36 | `INSERT` into `credentials`, including `redirect_uri`.                                                                                                                   |
| `utils/credentials_scanner.go`  |    42 | `CredentialsColumns` and the matching scanner.                                                                                                                           |
| `utils/const.go`                |     8 | Shared storage layout for `date_created`.                                                                                                                                |
| `cmd/genkey/main.go`            |    92 | Key generation tool. Writes the private key at 0600, refuses to overwrite.                                                                                               |
| `database/Schema.sql`           |    65 | `credentials` and `auth_codes`, with rationale comments.                                                                                                                 |
| `templates/ConsentPage.html`    |   197 | Consent form. Posts to `/authorize/consent`; echoes every authorize parameter.                                                                                           |
| `tests/oauth_flow_test.go`      |   794 | 18 end-to-end tests over real HTTP, real SQLite, real RSA.                                                                                                               |

### `pkg/authn` — shared token format

| File                 | Lines | Role                                                                                                                        |
| -------------------- | ----: | --------------------------------------------------------------------------------------------------------------------------- |
| `keys.go`            |   209 | `LoadKeyPair` (PKCS#1/PKCS#8/PKIX), `GenerateKeyPair`, PEM encoders, `Thumbprint` (RFC 7638).                               |
| `jwks.go`            |   137 | `JWK`/`JWKS` types, `NewJWKS`, `JWKSBytes`, `ParseJWKS`, `KeyByID`, `PublicKey`. No private-key field exists.               |
| `claims.go`          |   183 | `Claims` and `VerifyClaims`. Implements `jwt.Claims`. Distinct error types for issuer, audience, expiry, nbf, token type.   |
| `signer.go`          |   123 | `Signer.Issue` — RS256 with `kid` header. Server-side only.                                                                 |
| `verifier.go`        |   255 | `Verifier.Verify`, `StaticKeySource`, `RotatingKeySource`. Pins `alg` from configuration, never from the token.             |
| `middleware.go`      |   270 | `Middleware`, `ExtractBearerToken`, `WithClaims`/`ClaimsFrom`, `JWKSFetcher`, `AllowAll`.                                   |
| `config.go`          |   121 | `Config`, `LoadConfig`, `Validate`. Requires issuer, audience and key path.                                                 |
| `random.go`          |    43 | `RandomToken`, `NewJTI`, `ClockSkew`, `DefaultAccessTokenTTL`.                                                              |
| `authn_test.go`      |   501 | 16 tests: round trip, algorithm confusion, `alg:none`, wrong issuer/audience, expiry, tampering, JWKS round trip, rotation. |
| `middleware_test.go` |   320 | 12 tests: missing/malformed/wrong-scheme headers, case-insensitivity, scopes, reason-redaction, JWKS rotation, `AllowAll`.  |

### `pkg/dbutil` — database setup

| File             | Lines | Role                                                                                                       |
| ---------------- | ----: | ---------------------------------------------------------------------------------------------------------- |
| `sqlite.go`      |   112 | `OpenSQLite` with WAL, `busy_timeout` and `BEGIN IMMEDIATE`; `ApplySchema`.                                |
| `sqlite_test.go` |   105 | 4 tests: pragmas actually applied, `_txlock=immediate` in the DSN, concurrent writers, schema application. |

### `pkg/DataManager` — shared persistence

| File                        | Lines | Role                                                                                                                                                |
| --------------------------- | ----: | --------------------------------------------------------------------------------------------------------------------------------------------------- |
| `data_manager_impl.go`      |   232 | `GetEntry` (parameterized, explicit columns), `ConsumeEntry` (atomic read-and-delete), `PushData`/`UpdateData`/`DeleteData`, identifier validation. |
| `data_manager_impl_test.go` |   245 | 8 tests including an 8-goroutine race on `ConsumeEntry`.                                                                                            |

### `nodes/Event_Queue` — first consumer

| File                      | Lines | Role                                                                                                     |
| ------------------------- | ----: | -------------------------------------------------------------------------------------------------------- |
| `eventcore/event_core.go` |   207 | Every service endpoint wrapped in `authn.Middleware`. Fails closed if verification cannot be configured. |

### Manifests

| File                              | Role                                                                                   |
| --------------------------------- | -------------------------------------------------------------------------------------- |
| `manifests/auth-deployment.yaml`  | Auth deployment. Key Secret mount, token config, health probes, `AUTH_PORT` fix.       |
| `manifests/event-deployment.yaml` | Event_Queue deployment. `AUTH_JWKS_URL`, `AUTH_ISSUER`, `AUTH_AUDIENCE`, health probe. |

---

## 7. Data model

### `credentials` — one row per client + user

| Column            | Type       | Notes                             |
| ----------------- | ---------- | --------------------------------- |
| `id`              | INTEGER PK | Autoincrement                     |
| `date_created`    | VARCHAR    | RFC 3339                          |
| `client_id`       | TEXT       | **UNIQUE**, opaque, server-issued |
| `client_secret`   | TEXT       | **bcrypt hash**                   |
| `client_name`     | TEXT       | Display name                      |
| `redirect_uri`    | TEXT       | Space-separated, exact-match only |
| `username`        | TEXT       | **UNIQUE**                        |
| `user_email`      | TEXT       |                                   |
| `hashed_password` | TEXT       | **bcrypt hash**, cost 12          |
| `service_id`      | TEXT       | Owning Piranid service            |

### `auth_codes` — single-use, PKCE-bound

| Column                    | Type    | Notes                 |
| ------------------------- | ------- | --------------------- |
| `code`                    | TEXT PK | 256-bit opaque random |
| `client_id`               | TEXT    | Binding               |
| `subject`                 | TEXT    | Binding               |
| `scope`                   | TEXT    | Binding               |
| `redirect_uri`            | TEXT    | Binding               |
| `code_challenge`          | TEXT    | PKCE                  |
| `code_challenge_method`   | TEXT    | Always `S256`         |
| `state`, `nonce`          | TEXT    | Echoed to client      |
| `issued_at`, `expires_at` | INTEGER | Unix seconds          |
| `consumed_at`             | INTEGER | Audit; normally NULL  |

Indexes on `expires_at` and `client_id`.

### SQLite configuration

`OpenSQLite` sets `journal_mode=WAL`, `busy_timeout=5000`, `foreign_keys=ON`,
`synchronous=NORMAL`, `_txlock=immediate`, and caps the pool at 8 connections.

Three separate problems had to be solved to make concurrent token requests work.
All three were found by a concurrency test, none by reading the code:

1. **`busy_timeout` was not being applied at all.** The driver reads repeated
   `_pragma=name(value)` parameters. The first implementation emitted
   `_pragma_name=value`, which the driver ignores without complaint. Every write
   therefore failed with `SQLITE_BUSY` the moment two requests overlapped.
   `TestPragmasAreActuallyApplied` reads the pragma back from a live connection so
   this cannot regress silently.

2. **`busy_timeout` alone is insufficient even when applied.** A deferred
   transaction (`BEGIN`) takes a read lock first and upgrades it to a write lock
   later. SQLite deliberately does _not_ invoke the busy handler for that
   upgrade, because the transaction may hold a read lock another writer is
   waiting on, so waiting could deadlock. The upgrade fails immediately with
   `SQLITE_BUSY`. `_txlock=immediate` (`BEGIN IMMEDIATE`) acquires the write lock
   up front, where the busy handler does apply.

3. **A `db.Ping()` on every call made things worse.** Each `Ping` was itself a
   query that could fail with `SQLITE_BUSY`, turning transient contention into a
   hard error. These checks were removed: they added no liveness information
   (the pool manages connections) and were a source of spurious failures.

Covered by `TestConcurrentWritersDoNotFail`,
`TestConcurrentTokenRequestsSucceed`, and
`TestConcurrentCodeRedemptionYieldsOneToken`.

---

## 8. Configuration

### Auth node (required)

| Variable                | Example                             | Notes                                                |
| ----------------------- | ----------------------------------- | ---------------------------------------------------- |
| `AUTH_ISSUER`           | `https://auth.piranid.local`        | `iss`. **Required.** Must be stable across restarts. |
| `AUTH_AUDIENCE`         | `event-queue`                       | `aud`. **Required.**                                 |
| `AUTH_PRIVATE_KEY_PATH` | `/etc/piranid/keys/jwt_private.pem` | **Required.**                                        |
| `AUTH_PUBLIC_KEY_PATH`  | `/etc/piranid/keys/jwt_public.pem`  | Optional; derived if absent.                         |
| `AUTH_PORT`             | `8081`                              | Default `8081`.                                      |

The node **refuses to start** without issuer, audience, and key path.

### Auth node (optional)

| Variable                    | Default               |
| --------------------------- | --------------------- |
| `ACCESS_TOKEN_TTL`          | `15m`                 |
| `AUTH_CODE_TTL`             | `2m`                  |
| `AUTH_CLOCK_SKEW`           | `30s`                 |
| `AUTH_DB_PATH`              | `auth.db`             |
| `AUTH_SCHEMA_PATH`          | `database/Schema.sql` |
| `REDIS_HOST` / `REDIS_PORT` | unset (cache unused)  |
| `OTEL_COLLECTOR_ADDR`       | `localhost:4317`      |

### Consumer services

| Variable                        | Notes                                                                          |
| ------------------------------- | ------------------------------------------------------------------------------ |
| `AUTH_JWKS_URL`                 | **Required.** Where to fetch public keys.                                      |
| `AUTH_ISSUER` / `AUTH_AUDIENCE` | **Required.** Must match the Auth node's.                                      |
| `AUTH_LEEWAY`                   | Default `30s`.                                                                 |
| `AUTH_ALLOW_ANONYMOUS`          | `true` disables verification. **Development only**; logs a warning at startup. |

A consumer **refuses to start** without these, rather than serving
unauthenticated.

### Key provisioning

```bash
make keys                    # → jwt-keys/jwt_private.pem (0600), jwt_public.pem

kubectl -n piranid create secret generic auth-signing-keys \
  --from-file=jwt_private.pem=jwt-keys/jwt_private.pem \
  --from-file=jwt_public.pem=jwt-keys/jwt_public.pem
```

`.gitignore` covers `*.pem`, `*.key`, `*.p12`, `*.pfx` and `jwt-keys/`, so a
generated key cannot be committed.

---

## 9. Security properties

Each property has a test that fails if it regresses.

| Property                       | Mechanism                                                                         | Test                                                                           |
| ------------------------------ | --------------------------------------------------------------------------------- | ------------------------------------------------------------------------------ |
| Consumers cannot mint tokens   | Only the public key is distributed                                                | Structural                                                                     |
| Algorithm confusion blocked    | `alg` pinned from config via `jwt.WithValidMethods`, plus an RSA method assertion | `TestVerifierRejectsHS256SignedWithPublicKey`                                  |
| Unsigned tokens blocked        | Same pin rejects `alg: none`                                                      | `TestVerifierRejectsAlgNone`                                                   |
| Wrong issuer rejected          | Exact `iss` comparison                                                            | `TestVerifierRejectsWrongIssuer`                                               |
| Wrong audience rejected        | Exact `aud` comparison                                                            | `TestVerifierRejectsWrongAudience`                                             |
| Expiry enforced                | `exp` with skew tolerance                                                         | `TestVerifierRejectsExpired`                                                   |
| Not-before enforced            | `nbf`                                                                             | `TestVerifierRejectsNotYetValid`                                               |
| Token kind enforced            | `token_use`                                                                       | `TestVerifierRejectsWrongTokenType`                                            |
| Payload tampering detected     | Signature covers the payload                                                      | `TestVerifierRejectsTamperedPayload`                                           |
| Codes are single-use           | Atomic `ConsumeEntry`                                                             | `TestAuthorizationCodeIsSingleUse`, `TestConsumeEntryIsSafeUnderConcurrency`   |
| PKCE is mandatory              | `S256` required, `plain` refused                                                  | `TestPlainCodeChallengeMethodIsRejected`, `TestMissingCodeChallengeIsRejected` |
| PKCE is enforced at redemption | Constant-time S256 compare                                                        | `TestWrongCodeVerifierIsRejected`                                              |
| Codes are client-bound         | `client_id` re-checked                                                            | `TestCodeBoundToIssuingClient`                                                 |
| Passwords actually checked     | bcrypt compare                                                                    | `TestWrongPasswordIsRejected`                                                  |
| No user enumeration            | Identical response for unknown user and bad password                              | `TestUnknownUserIsIndistinguishableFromWrongPassword`                          |
| No open redirect               | `redirect_uri` exact-match; errors rendered locally when unvalidated              | `TestUnregisteredRedirectURIIsRejected`                                        |
| No secret in storage           | bcrypt at cost 12                                                                 | `TestPasswordIsStoredHashed`, `TestClientSecretIsStoredHashed`                 |
| Private keys never published   | `JWK` has no private field                                                        | `TestJWKSEndpointPublishesOnlyPublicKeys`                                      |
| No token values logged         | Metadata only                                                                     | Grep                                                                           |
| Rejection reasons not leaked   | Uniform `invalid_token` body                                                      | `TestRejectionBodyDoesNotLeakReason`                                           |
| SQL injection blocked          | Identifiers validated, values bound                                               | `TestGetEntryRejectsInjectedIdentifier`                                        |
| Concurrent writes do not fail  | `BEGIN IMMEDIATE` + `busy_timeout`                                                | `TestConcurrentWritersDoNotFail`, `TestConcurrentTokenRequestsSucceed`         |

---

## 10. Verification

```bash
make check          # fmt-check + vet + test, every module
make test-race      # race detector
```

Current state: **all green.** 60 tests, stable across repeated runs and under
the race detector.

| Suite              | Count | Coverage                                                                     |
| ------------------ | ----: | ---------------------------------------------------------------------------- |
| `pkg/authn`        |    28 | Signing, verification, all rejection paths, JWKS, rotation, middleware       |
| `pkg/DataManager`  |     8 | Parameterization, injection rejection, atomic consume under 8-goroutine race |
| `pkg/dbutil`       |     4 | Pragmas actually applied, `BEGIN IMMEDIATE`, concurrent writers, schema      |
| `nodes/Auth/tests` |    20 | Full OAuth flow end to end over HTTP, including concurrency                  |

`nodes/Auth/tests` runs against a real SQLite file, a real 2048-bit RSA key and a
real `httptest` server. Nothing is mocked, because the bugs were in the wiring
between storage, PKCE, signing and redirects.

### Live server verification

Also verified against a running binary: registration, consent with wrong and
correct passwords, code exchange, token contents, replay rejection, PKCE rejection,
unregistered-redirect rejection, method routing (405), JWKS output, and graceful
shutdown on SIGTERM.

Sample issued token:

```
Header:  {"alg":"RS256","kid":"uVO92HL3E17imBv4qbaDpOVlRV8MH1a6upqR2f9MluQ","typ":"JWT"}
Payload: iss=https://auth.piranid.test  aud=event-queue  sub=alice
         exp/iat/nbf set  jti=k5u4K_cj-ZOwUMcfXpzrOw
         scope=events:read  client_id=AaWzNni2ycum2A2xORXLYg  token_use=access
```

### Worked example

```bash
# 1. Generate keys
make keys

# 2. Run the server
cd nodes/Auth && AUTH_PORT=8099 \
  AUTH_ISSUER=https://auth.piranid.local \
  AUTH_AUDIENCE=event-queue \
  AUTH_PRIVATE_KEY_PATH=../../jwt-keys/jwt_private.pem \
  AUTH_DB_PATH=/tmp/auth.db ./auth_server

# 3. Register a client (client_secret shown once)
curl -X POST localhost:8099/register \
  -d client_name="Smoke Test" \
  -d redirect_uri=http://localhost:9999/callback \
  -d username=alice -d password=hunter2-correct \
  -d user_email=alice@piranid.test

# 4. Client computes its PKCE challenge
VERIFIER=$(head -c 48 /dev/urandom | base64 | tr -d '=+/' | head -c 64)
CHALLENGE=$(printf '%s' "$VERIFIER" | openssl dgst -sha256 -binary \
             | base64 | tr '+/' '-_' | tr -d '=')

# 5. Send the user to /authorize  →  consent page renders

# 6. POST the consent form  →  302 with ?code=…&state=…

# 7. Exchange over the back channel
curl -X POST localhost:8099/token \
  -d grant_type=authorization_code -d code=$CODE \
  -d redirect_uri=http://localhost:9999/callback \
  -d client_id=$CID -d client_secret=$CS \
  -d code_verifier=$VERIFIER
```

---

## 11. Deferred work

Recorded so these are decisions, not oversights.

| Item                         | Why deferred                                                                                                                                                                                   | What is needed                                                                                                   |
| ---------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------------- |
| **Refresh tokens**           | A second subsystem with no existing scaffold: storage table, rotation policy, reuse detection (RFC 6819 §5.2.2.3). `/token` currently returns `unsupported_grant_type` rather than pretending. | New table, `refresh_token` grant, rotation with family revocation, logout wired to revocation.                   |
| **Notifications auth**       | The node is gRPC-only (`main.go:38` `grpc.NewServer()`, no HTTP routes), so HTTP middleware does not apply.                                                                                    | `grpc.UnaryInterceptor` reading the bearer token from metadata, plus a decision on how callers obtain a token.   |
| **Key rotation**             | The verifier refetches on unknown `kid`, so consumers are ready. The auth node still serves exactly one key from one file.                                                                     | Publish both current and next key in the JWKS during a rotation window; switch signing keys; retire the old one. |
| **TLS termination**          | `main.go` still carries the original TODO. Credentials and codes travel in cleartext.                                                                                                          | Ingress or mesh TLS. Blocking for anything beyond a trusted LAN.                                                 |
| **Client credentials grant** | Useful for service-to-service tokens with no user.                                                                                                                                             | A client-only registration path; tokens with `sub` = client id.                                                  |
| **Discovery document**       | `/.well-known/openid-configuration` would make `jwks_uri` discoverable. Not implied by the current scope.                                                                                      | `issuer`, `authorization_endpoint`, `token_endpoint`, `jwks_uri`.                                                |
| **OpenID Connect**           | Out of scope; this is OAuth 2.0, not an identity provider.                                                                                                                                     | ID tokens, `/userinfo`, nonce validation.                                                                        |
| **Scope enforcement**        | Scopes are issued and carried, and the middleware can require them, but no scope registry exists.                                                                                              | A `scopes` table or a configured static set; per-route requirements.                                             |
| **Expired-code sweeper**     | `AuthCodeSweeper` exists but nothing schedules it.                                                                                                                                             | An interval task, or a cleanup on startup.                                                                       |
| **Redis caching**            | Wired but unused. Codes are read from SQLite, which is correct and simple.                                                                                                                     | Only if code redemption latency becomes a problem.                                                               |

### Known limitations

- **Single replica.** SQLite holds the client registry and unconsumed codes, so
  only one process may own the database file. Scaling out requires moving both to
  a shared store.
- **No revocation.** Access tokens are valid until `exp`. Logout is a no-op.
- **Registration is unauthenticated.** Anyone who can reach `/register` can create
  a client. Appropriate on a trusted LAN, not on a public network.
- **`authn` depends on `golang-jwt`.** Hand-rolled JWK encoding is small and
  tested, but a dedicated JOSE library would be the safer long-term choice.
