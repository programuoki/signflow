-- +goose Up
-- +goose StatementBegin
-- Append-only audit trail. Every state change lands exactly one row here.
--
-- WHO — the actor problem. An actor is EITHER a registered user (the owner) OR a
-- signer (accountless, identified only by their invite) OR the system (automatic
-- transitions) OR anonymous (a rejected token). We model that honestly instead of
-- forcing a user_id everywhere:
--   * actor_type    — the discriminator
--   * actor_user_id / actor_signer_id — nullable, plain UUIDs (NO foreign keys)
--   * actor_label   — the human identity FROZEN at event time
-- The ids are FK-free on purpose: an audit row must be self-contained and outlive
-- whatever it references. actor_label is what the UI shows, so the trail stays
-- readable with no joins even after the user/signer/document is gone.
--
-- document_id is likewise a plain UUID, NOT a foreign key: when a draft is
-- deleted its audit rows MUST survive. A cascade would erase the evidence; a
-- restrict would block the delete. FK-free keeps the rows and their document_id.
--
-- message is a frozen, plain-language sentence composed at write time. The UI
-- renders it verbatim (never raw JSON), and it can't drift if display code changes.
CREATE TABLE audit_events (
    -- Monotonic sequence: several events can share a transaction (e.g. signed +
    -- completed), so now() ties. seq gives a stable total order.
    seq             BIGINT      GENERATED ALWAYS AS IDENTITY,
    id              UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    document_id     UUID        NOT NULL,
    event_type      TEXT        NOT NULL,
    message         TEXT        NOT NULL,
    actor_type      TEXT        NOT NULL CHECK (actor_type IN ('user', 'signer', 'system', 'anonymous')),
    actor_user_id   UUID,
    actor_signer_id UUID,
    actor_label     TEXT        NOT NULL,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_audit_events_document ON audit_events (document_id, seq);
-- +goose StatementEnd

-- +goose StatementBegin
-- Enforce append-only IN THE DATABASE. We use a trigger rather than REVOKE
-- because the app connects as a superuser in dev and superusers bypass table
-- grants — the trigger's RAISE applies to everyone. It also fails loudly with an
-- error, unlike a rewrite RULE which would silently turn UPDATE/DELETE into a
-- no-op. (Honest limit: a superuser could still DROP this trigger; true
-- immutability against a hostile DBA needs off-box log shipping. This stops the
-- application — and ordinary roles — from ever rewriting history.)
CREATE FUNCTION audit_events_append_only() RETURNS trigger AS $$
BEGIN
    RAISE EXCEPTION 'audit_events is append-only: % is not permitted', TG_OP;
END;
$$ LANGUAGE plpgsql;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER audit_events_no_update_delete
    BEFORE UPDATE OR DELETE ON audit_events
    FOR EACH ROW EXECUTE FUNCTION audit_events_append_only();
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER audit_events_no_truncate
    BEFORE TRUNCATE ON audit_events
    FOR EACH STATEMENT EXECUTE FUNCTION audit_events_append_only();
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE audit_events;
DROP FUNCTION IF EXISTS audit_events_append_only();
-- +goose StatementEnd
