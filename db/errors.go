package db

import (
	"errors"

	"github.com/jackc/pgx/v5/pgconn"
)

func SQLState(err error) string {
	var postgres *pgconn.PgError
	if errors.As(err, &postgres) {
		return postgres.Code
	}
	return ""
}

func ConstraintName(err error) string {
	var postgres *pgconn.PgError
	if errors.As(err, &postgres) {
		return postgres.ConstraintName
	}
	return ""
}

func IsUniqueViolation(err error) bool { return SQLState(err) == "23505" }

func IsRetryable(err error) bool {
	switch SQLState(err) {
	case "40001", "40P01":
		return true
	default:
		return false
	}
}
