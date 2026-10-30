-- Whose a device is, for households that share a drawer of cables.
ALTER TABLE devices ADD COLUMN owner TEXT NOT NULL DEFAULT '';
