-- +goose Up
-- +goose StatementBegin
-- Single-use, expiring password-reset tokens. Same pattern as sessions: the
-- emailed link carries the raw token, the database stores only its hash.
CREATE TABLE password_reset_tokens (
    id         UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    token_hash TEXT        NOT NULL UNIQUE,
    user_id    UUID        NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    expires_at TIMESTAMPTZ NOT NULL,
    used_at    TIMESTAMPTZ
);
CREATE INDEX idx_password_reset_tokens_user_id ON password_reset_tokens(user_id);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE password_reset_tokens;
-- +goose StatementEnd
