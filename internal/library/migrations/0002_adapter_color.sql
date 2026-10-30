-- Adapters get a colour like cables, so both inventories share one shape.
ALTER TABLE adapter_items ADD COLUMN color TEXT NOT NULL DEFAULT '';
