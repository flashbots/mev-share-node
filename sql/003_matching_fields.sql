ALTER TABLE sbundle ADD COLUMN prematched bool NOT NULL default false;
ALTER TABLE sbundle ADD COLUMN matching_hash bytea;
CREATE INDEX CONCURRENTLY ON sbundle USING HASH (matching_hash) WHERE matching_hash is not null;