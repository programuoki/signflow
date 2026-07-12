-- +goose Up
-- +goose StatementBegin
-- A document is any uploaded file, owned by one user. We keep the SHA-256 hash
-- computed at upload time so integrity can be re-checked later (and so a
-- signature can pin the exact bytes that were signed).
--
-- status uses TEXT + CHECK rather than a Postgres ENUM: the set is small and
-- fixed, and TEXT+CHECK is easier to extend later without an ALTER TYPE dance.
-- The lifecycle is draft -> sent -> completed.
CREATE TABLE documents (
    id           UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    owner_id     UUID        NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    filename     TEXT        NOT NULL,
    content_type TEXT        NOT NULL,
    size         BIGINT      NOT NULL,
    file_hash    TEXT        NOT NULL,               -- SHA-256, hex
    storage_key  TEXT        NOT NULL,               -- opaque key into the file store
    status       TEXT        NOT NULL DEFAULT 'draft'
                 CHECK (status IN ('draft', 'sent', 'completed')),
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_documents_owner_id ON documents(owner_id);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE documents;
-- +goose StatementEnd
