# SignFlow

A document-signing workflow app: upload a document, invite signers by email, they
sign, and every action lands in an append-only audit trail.

SignFlow is the **reference application for the Programuoki server-side web track**.
It is a real, deployed system built to be read as much as run.

**🌐 Live:** https://signflow-production-67f3.up.railway.app

## ⚠️ Signatures here are simplified — read this first

A "signature" in SignFlow is a **typed name + a timestamp + the SHA-256 hash of the
document at the moment of signing**. That combination gives you *tamper-evident
integrity*: if the stored file changes by a single byte, its hash no longer matches
what was recorded at signing time, and the mismatch is detectable.

That is **all** it gives you. SignFlow is:

- **NOT** real public-key cryptography (PKI). No signer keypairs, no certificates.
- **NOT** an [eIDAS](https://en.wikipedia.org/wiki/EIDAS) advanced or **qualified**
  electronic signature.
- **NOT** legally equivalent to a handwritten signature in most jurisdictions.
- **NOT** proof of *who* signed — only that *someone holding the emailed link* typed
  a name at a time, and that the document is unchanged since.

Real e-signature platforms (DocuSign, qualified EU trust service providers) bind a
verified identity to a document with cryptographic keys and audited processes.
SignFlow deliberately stops at integrity so the mechanics stay teachable. Don't ship
it as a legal signing product.

## Stack

| Concern            | Choice                                                    |
| ------------------ | --------------------------------------------------------- |
| Language           | Go                                                        |
| Router             | [Chi](https://github.com/go-chi/chi)                      |
| Templates          | [Templ](https://templ.guide) (type-safe, compiled)        |
| Interactivity      | [HTMX](https://htmx.org) (vendored, no build step)        |
| Queries            | [sqlc](https://sqlc.dev) (schema-first, generated Go)     |
| Migrations         | [goose](https://github.com/pressly/goose) (embedded)      |
| Database           | PostgreSQL                                                |
| Auth               | **Server-side sessions in an HttpOnly cookie** (not JWT)  |
| Deploy             | Railway + Postgres addon                                  |

### Why sessions, not JWT?

This is deliberate. The Programuoki track pairs SignFlow with the *Pica* Android app,
which uses JWT. Same problem — "prove who this request is from" — two correct answers:

- A **stateless token in a header** suits a mobile client talking to an API.
- A **server-side session referenced by a cookie** suits a server-rendered web app:
  the session lives in Postgres, the browser only holds an opaque ID, and logout /
  revocation is a single `DELETE`. Cookies are `HttpOnly`, `SameSite=Lax`, and
  `Secure` in production.

So SignFlow implements sessions idiomatically — not a JWT wearing a cookie costume.

### Auth details

- **Password hashing: bcrypt** (`golang.org/x/crypto/bcrypt`, cost 12). Chosen over
  argon2id for a teaching codebase because it has one tuning knob instead of three,
  embeds its cost in the hash string (trivial verification and rehashing), and ships
  in the Go team's `x/crypto`. The honest tradeoff — argon2id is memory-hard and is
  OWASP's first pick for new systems, and bcrypt truncates input at 72 bytes — is
  handled by capping password length so nothing is silently truncated, and noted as
  the upgrade path. See [`internal/auth/password.go`](internal/auth/password.go).
- **Session tokens are hashed at rest.** The cookie carries a 256-bit random token;
  the `sessions` table stores only its SHA-256 hash. A database dump yields no usable
  sessions.
- **Cookie flags:** `HttpOnly` always, `SameSite=Lax` always, `Secure` in production.
  30-day lifetime. Verified: dev emits `HttpOnly; SameSite=Lax`, prod adds `Secure`.
- **CSRF:** `gorilla/csrf` on every unsafe method, token embedded as a hidden field in
  each form (and the logout button). Production keeps its strict Origin/Referer check;
  local plaintext HTTP is explicitly marked so dev works without HTTPS.

> **📎 Teaching note — the "referer not supplied" gotcha (for the CSRF lesson).**
> Submit a form on `http://localhost` with `gorilla/csrf` at defaults and you get a
> `403 Forbidden - referer not supplied` (or `referer invalid`). This is not a bug —
> it's CSRF protection doing its job. gorilla/csrf v1.7.3 assumes it's serving over
> HTTPS and, for unsafe methods, demands the request's `Origin`/`Referer` prove it
> came from the same secure origin (so an HTTP machine-in-the-middle can't forge a
> submission). Plain-HTTP localhost has no such proof, so it's rejected. The fix is
> to *tell* the middleware the request really is plaintext HTTP via
> `csrf.PlaintextHTTPRequest(r)` — which we do **in dev only** (see
> [`internal/handlers/router.go`](internal/handlers/router.go)), leaving the strict
> check fully armed in production. The lesson: CSRF defense is about *proving
> same-origin intent*, and the token is only half of it — the Origin/Referer check is
> the other half.
- **Account-enumeration resistant:** login uses one generic error for every failure,
  and "forgot password" always shows the same confirmation whether or not the email
  exists.
- **Password reset:** single-use, 1-hour token (hash stored, raw token in the link).
  Completing a reset burns all outstanding tokens and deletes all of that user's
  sessions.

## Documents & file storage

Uploading accepts **any file type** (SignFlow is not a PDF tool). On upload the file
is streamed to storage and **SHA-256 hashed in the same pass** — never fully buffered
in memory — and the hash is stored on the record. That hash is what later lets a
signer pin the exact bytes they signed, and lets anyone re-verify the file is
unchanged.

- **Where files live:** an `internal/storage.Store` interface with a local-disk
  implementation (`LocalStore`), mirroring the email pattern. Dev writes to `./uploads`.
- **Business rule:** only a **draft** can be deleted. Once a document is `sent`, its
  record is immutable (the delete query itself filters on `status = 'draft'`).
- **Authorization:** every document read/delete is **owner-scoped in the SQL query**
  (`WHERE ... AND owner_id = $me`), so one user literally cannot load another user's
  document — a foreign ID returns 404, not 403 (we don't leak existence). Verified
  with a second account.

> **⚠️ Railway's filesystem is ephemeral — decide this now, not at deploy.**
> A container's local disk is wiped on every deploy and restart. `LocalStore` writing
> to `./uploads` is fine for dev but on Railway your uploaded files would vanish on the
> next deploy. For production you must either (a) attach a **Railway Volume** and point
> `UPLOAD_DIR` at its mount path, or (b) implement `storage.Store` against object
> storage (S3 / Cloudflare R2). The interface exists precisely so this is a swap, not a
> rewrite. We'll make the call in the deploy phase; flagging it here so it's a decision,
> not a surprise.

## Signing

Signing is the heart of the app, and where the honesty caveat is earned.

**The flow.** The owner opens a draft and invites signers by email (one or many).
Sending flips the document **draft → sent** and emails each signer a private
tokened link. A signer **is not a user** — no account, no password. They open the
link, download and review the document, type their name, and sign. When the last
outstanding signer signs, the document flips **sent → completed**. The owner's
document page shows the full roster: who has signed, their typed name, and the
timestamp.

**What a signature is.** `typed name + timestamp + SHA-256 of the document at
signing time`, captured on the signer's row. The hash is recomputed from the stored
bytes at the moment of signing — not trusted from the record — so it attests to what
was actually there. See the caveat at the top of this README: this is tamper-evident
integrity and nothing more.

**Tamper-evidence, demonstrated.** Every time a document is shown (owner page or
signing page) SignFlow re-hashes the stored file and compares it to the hash recorded
at upload. If they differ, a loud **⚠ TAMPER DETECTED** banner appears with both
hashes. That is the *entire* security guarantee, made visible: change one byte of the
stored file and the app tells you. (Verified by appending a byte to the file on disk
and reloading.)

### Token design (a deliberate lesson)

- **The token is the authorization.** A signer presents only the token in their URL.
  It maps to exactly one `signers` row (the `token_hash` column is `UNIQUE`), so a
  signer structurally cannot reach another signer's row or another document — there is
  no document/row ID in the request to tamper with. A garbage token is a 404.
- **We store the hash, never the token.** Same as sessions and password resets: the
  emailed link carries the raw token; the database keeps only its SHA-256. A DB leak
  exposes no working links.
- **Single-use for *signing*, reusable for *viewing*.** The link stays openable so a
  signer can reopen it, re-download, and read before committing — but the signing
  action is single-use. The `UPDATE ... WHERE id = $1 AND status = 'pending'` guard
  means a second submit updates zero rows; the page just shows the already-signed
  state (idempotent). Re-posting a spent token creates no second signature.
- **Bounded validity: 30 days.** Signers take days, not minutes, so expiry is generous
  — but it *is* bounded, so a leaked link isn't useful forever. Expired links show an
  "expired" page instead of the form.

**Atomicity.** Inviting (create N signers + flip to sent) and signing (record
signature + maybe flip to completed) each run in a single Postgres transaction, so a
half-finished invite can't leave a document `sent` with no signers.

## Audit trail

Every state change lands exactly one **append-only** row in `audit_events`: who,
what, when. The owner's document page renders it as a chronological, plain-language
event log (never raw JSON). Captured events: `document.uploaded`, `document.sent`,
`signer.viewed`, `document.signed`, `document.completed`, `document.deleted`, plus the
security-relevant `signer.bad_token` (a spent/expired link re-used) and
`document.tamper_detected` (integrity check failed on view).

**WHO — the actor problem.** An actor is *either* a registered user (the owner) *or* a
signer (accountless) *or* the system (automatic transitions) *or* anonymous. Rather
than force everything into a `user_id`, each row carries:
`actor_type` (the discriminator) + nullable `actor_user_id` / `actor_signer_id` + a
frozen `actor_label`. The ids are **FK-free plain UUIDs** so a row is self-contained,
and `actor_label` (the email captured at event time) is what the UI shows — the trail
stays readable with no joins even after the user/signer is gone.

**Rows outlive what they describe.** `document_id` is also an FK-free UUID: deleting a
draft removes the `documents` row but its audit events remain (a cascade would erase
the evidence; a restrict would block the delete). Verified — after deleting a draft,
its `uploaded` and `deleted` events are still queryable.

**Append-only, enforced in the database.** A `BEFORE UPDATE OR DELETE`/`TRUNCATE`
trigger `RAISE`s an exception, so `UPDATE`, `DELETE`, and `TRUNCATE` on `audit_events`
all fail loudly:

```
ERROR:  audit_events is append-only: UPDATE is not permitted
```

Why a trigger and not `REVOKE`? The app connects as a **superuser** in dev, and
superusers bypass table grants — the trigger's `RAISE` applies to everyone. It also
fails loudly, unlike a rewrite `RULE` which would silently turn a mutation into a
no-op. **Honest limit:** a superuser could still `DROP` the trigger; true immutability
against a hostile DBA needs off-box log shipping. This stops the *application* — and
any ordinary role — from ever rewriting history, which is the threat model that
matters here.

**Atomicity.** Each event is written in the **same transaction** as the state change
it records (upload, invite, sign, delete), so a committed change can never be missing
from the trail, and a rolled-back one leaves no phantom event. Read-path events
(`viewed`, `tamper_detected`) are best-effort and only logged on failure.

**Ordering: a `seq` column, not the timestamp.** `now()` is the *transaction start*
time in Postgres, so two events in one transaction (e.g. `signed` + `completed`) share
a `created_at` and can't be ordered by it. A monotonic `seq BIGINT GENERATED ALWAYS AS
IDENTITY` gives a stable total order; the UI sorts by it. (Timestamps are still shown —
they're just not the sort key.)

## Domain

- **users** — registration, login, logout, password reset.
- **documents** — owner, uploaded file, SHA-256 `file_hash`, status `draft → sent → completed`.
- **signers** — invited by email, reached via a tokened link, status `pending → signed`.
- **audit_events** — append-only record of who did what, when. Every state change.

## Project layout

```
cmd/signflow/         # main: config, migrate, connect, serve
internal/
  config/             # env-based configuration
  db/                 # sqlc-generated queries + pgx pool  (queries/ holds .sql sources)
  handlers/           # HTTP handlers, router, middleware
  web/                # templ templates (.templ + generated _templ.go)
db/migrations/        # goose SQL migrations (embedded into the binary)
static/assets/        # CSS + vendored htmx
sqlc.yaml             # sqlc config (reads schema from the migrations)
```

## Local development

### Prerequisites

- Go 1.26+
- PostgreSQL running locally
- `templ` and `sqlc` on your PATH only if you plan to regenerate code
  (the generated `.go` files are committed, so you can build without them)

### Run it

```bash
# 1. Create a database
createdb signflow

# 2. Configure (optional — defaults target a local trust-auth Postgres)
cp .env.example .env

# 3. Run. Migrations apply automatically on startup.
go run ./cmd/signflow
# → http://localhost:8080
```

The default `DATABASE_URL` is
`postgres://postgres@localhost:5432/signflow?sslmode=disable`.

> No Docker? On Windows you can get a user-level Postgres with
> `scoop install postgresql` (no admin needed), then
> `pg_ctl -D "$env:USERPROFILE\scoop\apps\postgresql\current\data" start`.

### Regenerating code

```bash
make generate   # templ generate + sqlc generate
```

### Email in development

Registration, password reset, and signer invites all send email. In dev the default
sender is a **console sender that prints the link straight to the terminal** — you can
run every flow end to end with **no API key**. Set `EMAIL_SENDER=resend` +
`RESEND_API_KEY` for real delivery in production.

## Deploy (Railway)

Live at **https://signflow-production-67f3.up.railway.app**, on
[Railway](https://railway.com) with a Postgres addon and a persistent Volume.

**How it builds and runs.**
- A multi-stage [`Dockerfile`](Dockerfile) produces a single static binary on
  `distroless/static`. Migrations and static assets are embedded (`//go:embed`), so
  the image ships nothing else, and the binary **auto-runs goose migrations on
  startup** — a fresh deploy migrates itself.
- [`railway.json`](railway.json) selects the Dockerfile builder and a `/healthz`
  healthcheck.

**Storage: a Railway Volume (not object storage).** Railway's container filesystem is
**ephemeral** — it's wiped on every deploy — so `LocalStore` writing to `./uploads`
would lose every upload on redeploy. A Volume is mounted at `/data` and `UPLOAD_DIR`
points at `/data/uploads`. Chosen over S3/R2 because it's one config change with no new
SDK or credentials, and the `storage.Store` interface already makes object storage a
clean swap if horizontal scaling (multiple instances) is ever needed — a Volume binds
to one instance. **Verified live:** upload a file, redeploy, download it again — same
bytes, same hash.

**Production configuration** (set on the Railway service; nothing is defaulted):

| Variable         | Value                                        | Notes                                   |
| ---------------- | -------------------------------------------- | --------------------------------------- |
| `APP_ENV`        | `prod`                                       | flips `Secure` cookies + strict CSRF    |
| `DATABASE_URL`   | `${{Postgres.DATABASE_URL}}`                 | reference to the Postgres addon         |
| `SESSION_SECRET` | *(generated, `openssl rand -base64 32`)*     | **required** in prod                    |
| `BASE_URL`       | `https://signflow-production-67f3.up.railway.app` | **required** in prod; used in email links |
| `UPLOAD_DIR`     | `/data/uploads`                              | the mounted Volume                      |
| `EMAIL_SENDER`   | `console` (default)                          | prints links to logs; set `resend` + `RESEND_API_KEY` for real email |
| `PORT`           | *(injected by Railway)*                      | app listens on `$PORT`                  |

The **console email sender is kept as the default even in production**, so the whole
flow (registration, password reset, signer invites) works with **no API key** — the
links print to the Railway deploy logs. Swap in Resend by setting `EMAIL_SENDER=resend`
and `RESEND_API_KEY`.

**Verified against the live URL** (not localhost): the session cookie carries
`Secure` in prod; gorilla/csrf's strict Origin/Referer check is active (a POST with no
`Origin`/`Referer` is rejected — the dev-only plaintext bypass is *not* in effect);
and the full flow runs end to end — register → password reset (link read from the
deploy logs) → upload → invite two signers → both sign → document `completed` → audit
trail intact — followed by a redeploy that left the uploaded file untouched.

**Reproducing the deploy** (Railway CLI):

```bash
railway login
railway init --name signflow
railway add --database postgres
railway add --service signflow \
  --variables APP_ENV=prod \
  --variables 'DATABASE_URL=${{Postgres.DATABASE_URL}}' \
  --variables UPLOAD_DIR=/data/uploads \
  --variables "SESSION_SECRET=$(openssl rand -base64 32)"
railway domain                       # note the URL, then:
railway variables --set "BASE_URL=https://<your-domain>.up.railway.app"
railway volume add --mount-path /data
railway up                           # builds the Dockerfile and deploys
```

## Build status

This app is built in phases:

1. ✅ **Skeleton** — module, Chi, Templ, goose, Postgres, one page rendering.
2. ✅ **Auth** — register / login / logout / password reset with server-side sessions, CSRF, bcrypt.
3. ✅ **Documents** — upload (any type), SHA-256 hash, owner-scoped dashboard, view, download, delete-draft.
4. ✅ **Signing** — invite (accountless tokened links), sign = name+time+doc hash, status transitions, tamper-evidence.
5. ✅ **Audit trail** — append-only `audit_events`, polymorphic actor, DB-enforced immutability, event-log UI.
6. ✅ **Deploy** — Railway (Dockerfile + Postgres addon + Volume), live and verified end to end.
