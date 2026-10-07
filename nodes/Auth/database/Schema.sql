-- Piranid Auth node schema.
--
-- This is a breaking migration from the original two-table schema. The old
-- definitions could not express the OAuth authorization-code flow:
--   * credentials.client_id was INTEGER but stored opaque string client ids
--   * credentials had no redirect_uri column at all, so the redirect_uri
--     equality checks in the handlers compared against "" and always failed
--   * auth_codes held only (authcode, expires); PKCE needs to bind the code to
--     the client, subject, redirect_uri and a code_challenge
--   * both tables stored secrets as plaintext

-- Registered OAuth clients together with their owning user.
-- One row = one client + one user. Client secrets and passwords are stored as
-- bcrypt hashes; the plaintext secret is shown exactly once at registration.
CREATE TABLE IF NOT EXISTS credentials (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    date_created    VARCHAR(255) NOT NULL,

    -- OAuth client. Opaque string identifier, issued by this server.
    client_id       TEXT    NOT NULL UNIQUE,
    client_secret   TEXT    NOT NULL,          -- bcrypt hash of the secret
    client_name     TEXT    NOT NULL DEFAULT '',
    redirect_uri    TEXT    NOT NULL,          -- space-separated, per RFC 6749 3.1.2

    -- Resource owner (the human who authenticates).
    username        TEXT    NOT NULL UNIQUE,
    user_email      TEXT    NOT NULL,
    hashed_password TEXT    NOT NULL,          -- bcrypt hash of the password

    -- Identifies the Piranid service this client belongs to.
    service_id      TEXT    NOT NULL
);

-- Single-use authorization codes, with PKCE binding (RFC 6749 4.1.2, RFC 7636).
-- The code is an opaque 256-bit random string, not a JWT: it must be
-- revocable by deletion, which a self-contained token cannot be.
CREATE TABLE IF NOT EXISTS auth_codes (
    -- Opaque random authorization code. PRIMARY KEY so a replay hits a
    -- constraint rather than silently returning a second row.
    code                  TEXT    PRIMARY KEY,

    -- Binding: every field below is fixed at /authorize time and re-checked at
    -- /token time. This is what stops a stolen code from being replayed
    -- against a different client or redirect target.
    client_id             TEXT    NOT NULL,
    subject               TEXT    NOT NULL,   -- username of the authenticated user
    scope                 TEXT    NOT NULL DEFAULT '',
    redirect_uri          TEXT    NOT NULL,

    -- PKCE (RFC 7636). code_challenge_method is always "S256"; "plain" is not
    -- accepted because it provides no protection against an intercepted code.
    code_challenge        TEXT    NOT NULL,
    code_challenge_method TEXT    NOT NULL,

    -- Echoed back verbatim to the client for its own CSRF/correlation use.
    state                 TEXT    NOT NULL DEFAULT '',
    nonce                 TEXT    NOT NULL DEFAULT '',

    -- Unix seconds.
    issued_at             INTEGER NOT NULL,
    expires_at            INTEGER NOT NULL,

    -- Set when the code is redeemed. Rows are deleted on consume, so this is
    -- normally NULL for live codes; it exists for audit and for sweeping
    -- partially-consumed rows.
    consumed_at           INTEGER
);

CREATE INDEX IF NOT EXISTS idx_auth_codes_expires_at ON auth_codes (expires_at);
CREATE INDEX IF NOT EXISTS idx_auth_codes_client_id  ON auth_codes (client_id);