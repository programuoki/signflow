# Multi-stage build. The final image is a single static binary on distroless —
# migrations and static assets are embedded (//go:embed), so nothing else ships.

# ---- build ----
FROM golang:1.26 AS build
WORKDIR /src

# Cache module downloads.
COPY go.mod go.sum ./
RUN go mod download

# Build. templ/sqlc output is committed, so this is a plain `go build`.
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/signflow ./cmd/signflow

# ---- runtime ----
# distroless/static ships CA certificates (needed for the Resend HTTPS call) and
# runs as root, so it can write to the mounted Railway Volume.
FROM gcr.io/distroless/static-debian12
COPY --from=build /out/signflow /signflow

# Railway overrides PORT at runtime; this is just the default/dev value.
ENV PORT=8080
EXPOSE 8080

ENTRYPOINT ["/signflow"]
