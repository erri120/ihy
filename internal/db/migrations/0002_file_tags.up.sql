-- 0002: raw tag evidence on file rows.
--
-- The scanner stores the complete raw tag map (multi-valued tags as
-- JSON arrays) per file. Tags are evidence, not truth: the resolver
-- links files into the graph from MBIDs, the raw map stays for
-- conflict reporting and debugging.
ALTER TABLE file ADD COLUMN tags jsonb NOT NULL DEFAULT '{}';
