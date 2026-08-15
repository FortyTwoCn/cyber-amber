-- The authenticated message feed returns item.type as an enum string on some
-- accounts (for example "reply"), while older fixtures used an integer. Keep
-- the legacy column for upgrade compatibility and persist the lossless value
-- in a text column.
ALTER TABLE mention_events ADD COLUMN business_type_text TEXT NOT NULL DEFAULT '';

UPDATE mention_events
SET business_type_text = CAST(business_type AS TEXT)
WHERE business_type_text = '';
