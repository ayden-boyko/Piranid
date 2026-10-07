CREATE TABLE IF NOT EXISTS notifications (
    -- Composite natural key. The schema previously declared id as an
    -- INTEGER PRIMARY KEY AUTOINCREMENT while every query bound entry.Id,
    -- a string, and filtered on a contact_info column that did not exist.
    service_id    TEXT    NOT NULL,
    contact_info  TEXT    NOT NULL,
    method        TEXT    NOT NULL,
    message_data  TEXT    NOT NULL DEFAULT '{}',
    importance    INTEGER NOT NULL DEFAULT 0,
    template      TEXT    NOT NULL DEFAULT '',
    sent          INTEGER NOT NULL DEFAULT 0,
    created_at    TEXT    NOT NULL,
    PRIMARY KEY (service_id, contact_info)
);

CREATE INDEX IF NOT EXISTS idx_notifications_sent ON notifications (sent);

-- Template registry.
--
-- This table was created and then never read or written, while every send
-- failed because no template could be resolved. It is the natural home for
-- per-service template overrides.
CREATE TABLE IF NOT EXISTS templates (
    service_id TEXT    NOT NULL,
    method     TEXT    NOT NULL,
    template   TEXT    NOT NULL,
    PRIMARY KEY (service_id, method)
);