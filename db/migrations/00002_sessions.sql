-- +goose Up
-- +goose StatementBegin
-- Server-side session store. The cookie holds an opaque random token; we store
-- only the SHA-256 hash of it, so a database leak does not hand out live
-- sessions. This is the "session in a cookie" half of the sessions-vs-JWT
-- contrast the course teaches.
CREATE TABLE sessions (
    id         UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    token_hash TEXT        NOT NULL UNIQUE,
    user_id    UUID        NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    expires_at TIMESTAMPTZ NOT NULL
);
CREATE INDEX idx_sessions_user_id ON sessions(user_id);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE sessions;
-- +goose StatementEnd
