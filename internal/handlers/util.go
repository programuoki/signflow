package handlers

import (
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

func chiURLParam(r *http.Request, key string) string {
	return chi.URLParam(r, key)
}

// isUniqueViolation reports whether err is a Postgres unique-constraint error
// (SQLSTATE 23505), so we can turn a duplicate-email race into a friendly message.
func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}
