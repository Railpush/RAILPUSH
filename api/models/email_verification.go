package models

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"strings"
	"time"

	"github.com/railpush/api/database"
	"github.com/railpush/api/utils"
)

var ErrInvalidEmailVerificationToken = errors.New("invalid or expired token")

func hashEmailVerificationToken(raw string) string {
	raw = strings.TrimSpace(raw)
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}

// IssueEmailVerificationToken creates a new single-use token for a user and returns the raw token
// to embed in a verification link.
func IssueEmailVerificationToken(userID string, ttl time.Duration) (string, error) {
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return "", errors.New("missing user_id")
	}
	if ttl <= 0 {
		ttl = 24 * time.Hour
	}
	expiresAt := time.Now().UTC().Add(ttl)

	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		raw, err := utils.GenerateRandomString(32) // 64 hex chars
		if err != nil {
			lastErr = err
			continue
		}
		h := hashEmailVerificationToken(raw)
		if h == "" {
			lastErr = errors.New("token hash failed")
			continue
		}
		if _, err := database.DB.Exec(
			"INSERT INTO email_verification_tokens (user_id, token_hash, expires_at) VALUES ($1, $2, $3)",
			userID, h, expiresAt,
		); err == nil {
			return raw, nil
		} else {
			lastErr = err
			continue
		}
	}
	if lastErr == nil {
		lastErr = errors.New("failed to issue token")
	}
	return "", lastErr
}

// ConsumeEmailVerificationToken marks a token as used and sets the user's email_verified_at.
// Returns the verified user_id on success.
func ConsumeEmailVerificationToken(rawToken string) (string, error) {
	rawToken = strings.TrimSpace(rawToken)
	if rawToken == "" {
		return "", ErrInvalidEmailVerificationToken
	}
	h := hashEmailVerificationToken(rawToken)
	if h == "" {
		return "", ErrInvalidEmailVerificationToken
	}

	tx, err := database.DB.Begin()
	if err != nil {
		return "", err
	}
	defer tx.Rollback()

	var userID string
	err = tx.QueryRow(
		`SELECT user_id::text
		   FROM email_verification_tokens
		  WHERE token_hash=$1
		    AND used_at IS NULL
		    AND expires_at > NOW()
		  ORDER BY created_at DESC
		  LIMIT 1
		  FOR UPDATE`,
		h,
	).Scan(&userID)
	if err == sql.ErrNoRows {
		return "", ErrInvalidEmailVerificationToken
	}
	if err != nil {
		return "", err
	}

	if _, err := tx.Exec("UPDATE email_verification_tokens SET used_at=NOW() WHERE token_hash=$1 AND used_at IS NULL", h); err != nil {
		return "", err
	}
	if _, err := tx.Exec("UPDATE users SET email_verified_at=COALESCE(email_verified_at, NOW()) WHERE id=$1", userID); err != nil {
		return "", err
	}

	if err := tx.Commit(); err != nil {
		return "", err
	}
	return userID, nil
}

