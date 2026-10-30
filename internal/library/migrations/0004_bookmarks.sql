-- A bookmark is a route you saved to look at again: a pair of devices,
-- stored by name so the route is worked out fresh from what you own.
CREATE TABLE bookmarks (
    id          TEXT PRIMARY KEY,
    name        TEXT NOT NULL DEFAULT '',
    from_device TEXT NOT NULL,
    to_device   TEXT NOT NULL,
    created_at  INTEGER NOT NULL,
    updated_at  INTEGER NOT NULL,
    rev         INTEGER NOT NULL DEFAULT 1,
    deleted     INTEGER NOT NULL DEFAULT 0
);
