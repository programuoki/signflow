# SignFlow

A document-signing workflow app: upload a document, invite signers by email, they
sign, and every action lands in an append-only audit trail.

SignFlow is the **reference application for the Programuoki server-side web track**.
It is a real, deployed system built to be read as much as run.

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

## Build status

This app is being built in phases:

1. ✅ **Skeleton** — module, Chi, Templ, goose, Postgres, one page rendering.
2. ⬜ Auth — register / login / logout / password reset with sessions.
3. ⬜ Documents — upload, hash, dashboard.
4. ⬜ Signing — invite, tokened link, sign, status transitions.
5. ⬜ Audit trail.
6. ⬜ Deploy to Railway.
