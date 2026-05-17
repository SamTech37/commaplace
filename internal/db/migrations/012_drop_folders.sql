-- Remove folder structure from notes. All folder_path values are blanked;
-- the column is kept in the schema for compatibility with the initial INSERT.
-- The effective unique constraint becomes (author_id, slug) since folder_path is always ''.
UPDATE notes SET folder_path = '';
UPDATE links SET target_folder_path = '';
