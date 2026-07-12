-- +goose Up
-- +goose StatementBegin
-- A signer is invited to sign one document. A signer is NOT a user: they have no
-- account. The tokened link is the entire authorization — whoever holds the raw
-- token may view and sign this one row. We store only the token's SHA-256 hash,
-- consistent with sessions and password resets.
--
-- The signature itself is captured inline on the row when they sign:
--   signed_name     — what they typed
--   signed_at       — when
--   signed_doc_hash — SHA-256 of the document AT SIGNING TIME
-- That triple is the whole (simplified) signature: tamper-evident integrity and
-- nothing more. See the README's honesty caveat.
CREATE TABLE signers (
    id              UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    document_id     UUID        NOT NULL REFERENCES documents(id) ON DELETE CASCADE,
    email           TEXT        NOT NULL,
    token_hash      TEXT        NOT NULL UNIQUE,
    status          TEXT        NOT NULL DEFAULT 'pending'
                    CHECK (status IN ('pending', 'signed')),
    signed_name     TEXT,
    signed_at       TIMESTAMPTZ,
    signed_doc_hash TEXT,
    expires_at      TIMESTAMPTZ NOT NULL,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    -- Can't invite the same email to the same document twice.
    UNIQUE (document_id, email)
);
CREATE INDEX idx_signers_document_id ON signers(document_id);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE signers;
-- +goose StatementEnd
