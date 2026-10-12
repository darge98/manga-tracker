-- The manga table becomes a lean cache of what cards and lists display, kept
-- only for titles some user references. Full details (synopsis, authors) are
-- always read live from MyAnimeList.
UPDATE manga SET metadata = metadata - 'description' - 'author';

-- Rows that carry no information are no longer stored.
DELETE FROM chapters WHERE NOT is_read AND rating IS NULL AND notes = '';
DELETE FROM library l
WHERE NOT l.to_read AND NOT l.favorite
  AND NOT EXISTS (SELECT 1 FROM chapters c WHERE c.user_id = l.user_id AND c.manga_id = l.manga_id);

-- Finding unreferenced manga, and deleting manga rows, look up references by manga_id.
CREATE INDEX IF NOT EXISTS library_manga_idx ON library(manga_id);
CREATE INDEX IF NOT EXISTS list_items_manga_idx ON list_items(manga_id);

DELETE FROM manga m
WHERE NOT EXISTS (SELECT 1 FROM library l WHERE l.manga_id = m.id)
  AND NOT EXISTS (SELECT 1 FROM list_items i WHERE i.manga_id = m.id);
