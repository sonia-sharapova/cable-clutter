-- The item a link actually uses while its system is up. It may differ from
-- preferred_item when that one is busy and another of the same type is
-- swapped in. Cleared when the system goes down.
ALTER TABLE links ADD COLUMN assigned_item TEXT;
CREATE INDEX links_assigned ON links(assigned_item) WHERE deleted = 0 AND assigned_item IS NOT NULL;
