-- Tag each external vault with the static-site generator it uses, so the
-- crawler can pick the right fetcher.
ALTER TABLE external_vaults ADD COLUMN kind TEXT NOT NULL DEFAULT 'publish';
CREATE INDEX idx_external_vaults_kind ON external_vaults(kind);
