-- Library schema v1: the user's own gear.
--
-- Every row has a UUIDv7 id, timestamps, a rev counter and a soft-delete
-- tombstone so rows can later sync to a server. Catalog entries are
-- referenced by stable string keys (e.g. 'usb-a:m|usb-c:m'), never row ids.
-- Cable status (free / reserved / in use) is derived from links and active
-- systems, never stored.

CREATE TABLE devices (
    id          TEXT PRIMARY KEY,
    model       TEXT NOT NULL,          -- catalog device model id
    nickname    TEXT NOT NULL DEFAULT '',
    notes       TEXT NOT NULL DEFAULT '',
    created_at  INTEGER NOT NULL,
    updated_at  INTEGER NOT NULL,
    rev         INTEGER NOT NULL DEFAULT 1,
    deleted     INTEGER NOT NULL DEFAULT 0
);

CREATE TABLE cable_items (
    id                TEXT PRIMARY KEY,
    part_key          TEXT NOT NULL,    -- catalog cable key
    nickname          TEXT NOT NULL DEFAULT '',
    color             TEXT NOT NULL DEFAULT '',
    location          TEXT NOT NULL DEFAULT '',
    notes             TEXT NOT NULL DEFAULT '',
    dedicated_device  TEXT REFERENCES devices(id),
    created_at        INTEGER NOT NULL,
    updated_at        INTEGER NOT NULL,
    rev               INTEGER NOT NULL DEFAULT 1,
    deleted           INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX cable_items_part ON cable_items(part_key) WHERE deleted = 0;

CREATE TABLE adapter_items (
    id                TEXT PRIMARY KEY,
    part_key          TEXT NOT NULL,    -- catalog adapter key
    nickname          TEXT NOT NULL DEFAULT '',
    location          TEXT NOT NULL DEFAULT '',
    notes             TEXT NOT NULL DEFAULT '',
    dedicated_device  TEXT REFERENCES devices(id),
    created_at        INTEGER NOT NULL,
    updated_at        INTEGER NOT NULL,
    rev               INTEGER NOT NULL DEFAULT 1,
    deleted           INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX adapter_items_part ON adapter_items(part_key) WHERE deleted = 0;

CREATE TABLE systems (
    id          TEXT PRIMARY KEY,
    name        TEXT NOT NULL,
    active      INTEGER NOT NULL DEFAULT 0,
    notes       TEXT NOT NULL DEFAULT '',
    created_at  INTEGER NOT NULL,
    updated_at  INTEGER NOT NULL,
    rev         INTEGER NOT NULL DEFAULT 1,
    deleted     INTEGER NOT NULL DEFAULT 0
);
CREATE UNIQUE INDEX systems_name ON systems(name) WHERE deleted = 0;

-- One cable or adapter joining two device ports within a system. The link
-- names a part type; preferred_item pins a specific physical item, and any
-- free item of the same type may be swapped in when it is busy.
CREATE TABLE links (
    id              TEXT PRIMARY KEY,
    system_id       TEXT NOT NULL REFERENCES systems(id),
    part_key        TEXT NOT NULL,
    preferred_item  TEXT,               -- cable_items.id or adapter_items.id
    from_device     TEXT REFERENCES devices(id),
    from_port       TEXT NOT NULL DEFAULT '',
    to_device       TEXT REFERENCES devices(id),
    to_port         TEXT NOT NULL DEFAULT '',
    created_at      INTEGER NOT NULL,
    updated_at      INTEGER NOT NULL,
    rev             INTEGER NOT NULL DEFAULT 1,
    deleted         INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX links_system ON links(system_id) WHERE deleted = 0;
CREATE INDEX links_item ON links(preferred_item) WHERE deleted = 0;

CREATE TABLE wishlist (
    id          TEXT PRIMARY KEY,
    part_key    TEXT NOT NULL,
    qty         INTEGER NOT NULL DEFAULT 1,
    system_id   TEXT REFERENCES systems(id),
    notes       TEXT NOT NULL DEFAULT '',
    done        INTEGER NOT NULL DEFAULT 0,
    created_at  INTEGER NOT NULL,
    updated_at  INTEGER NOT NULL,
    rev         INTEGER NOT NULL DEFAULT 1,
    deleted     INTEGER NOT NULL DEFAULT 0
);

-- Connectors, cables, adapters or devices the user defines when the bundled
-- catalog lacks them. Same shape as catalog YAML entries, stored as JSON;
-- lookups check these before the bundled catalog.
CREATE TABLE custom_catalog (
    kind        TEXT NOT NULL,          -- connector | cable | adapter | device
    id          TEXT NOT NULL,          -- connector id or part key
    body        TEXT NOT NULL,          -- JSON
    created_at  INTEGER NOT NULL,
    updated_at  INTEGER NOT NULL,
    rev         INTEGER NOT NULL DEFAULT 1,
    deleted     INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (kind, id)
);

-- Append-only log of every write: the future sync cursor.
CREATE TABLE changes (
    seq     INTEGER PRIMARY KEY AUTOINCREMENT,
    tbl     TEXT NOT NULL,
    row_id  TEXT NOT NULL,
    op      TEXT NOT NULL,              -- insert | update | delete
    at      INTEGER NOT NULL
);
