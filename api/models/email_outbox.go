package models

import (
	"database/sql"
	"strings"
	"time"

	"github.com/lib/pq"
	"github.com/railpush/api/database"
)

type EmailOutboxMessage struct {
	ID          string     `json:"id"`
	DedupeKey   string     `json:"dedupe_key,omitempty"`
	MessageType string     `json:"message_type"`
	ToEmail     string     `json:"to_email"`
	Subject     string     `json:"subject"`
	BodyText    string     `json:"body_text"`
	BodyHTML    string     `json:"body_html"`
	Status      string     `json:"status"`
	Attempts    int        `json:"attempts"`
	LastError   string     `json:"last_error,omitempty"`
	NextAttempt time.Time  `json:"next_attempt_at"`
	CreatedAt   time.Time  `json:"created_at"`
	SentAt      *time.Time `json:"sent_at,omitempty"`
}

func nullStringOrNil(raw string) interface{} {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	return raw
}

// EnqueueEmail inserts a message into the outbox. If dedupeKey is non-empty and already exists,
// the insert is ignored (idempotency).
func EnqueueEmail(dedupeKey, messageType, toEmail, subject, bodyText, bodyHTML string) (string, error) {
	dedupeKey = strings.TrimSpace(dedupeKey)
	messageType = strings.TrimSpace(messageType)
	toEmail = strings.TrimSpace(toEmail)
	subject = strings.TrimSpace(subject)

	if messageType == "" || toEmail == "" || subject == "" {
		return "", nil
	}

	var id string
	err := database.DB.QueryRow(
		`INSERT INTO email_outbox (dedupe_key, message_type, to_email, subject, body_text, body_html)
		 VALUES ($1,$2,$3,$4,$5,$6)
		 ON CONFLICT (dedupe_key) WHERE dedupe_key IS NOT NULL DO NOTHING
		 RETURNING id`,
		nullStringOrNil(dedupeKey), messageType, toEmail, subject, bodyText, bodyHTML,
	).Scan(&id)
	if err == sql.ErrNoRows {
		// Duplicate (dedupe hit).
		return "", nil
	}
	return id, err
}

// ClaimEmailOutboxBatch leases a batch of pending messages for delivery.
func ClaimEmailOutboxBatch(owner string, limit int, leaseSeconds int, maxAttempts int) ([]EmailOutboxMessage, error) {
	owner = strings.TrimSpace(owner)
	if owner == "" {
		owner = "railpush"
	}
	if limit <= 0 {
		limit = 1
	}
	if leaseSeconds <= 0 {
		leaseSeconds = 120
	}

	tx, err := database.DB.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	rows, err := tx.Query(
		`SELECT id, COALESCE(dedupe_key,''), COALESCE(message_type,''), COALESCE(to_email,''), COALESCE(subject,''),
		        COALESCE(body_text,''), COALESCE(body_html,''), COALESCE(status,'pending'), COALESCE(attempts,0),
		        COALESCE(last_error,''), next_attempt_at, created_at, sent_at
		   FROM email_outbox
		  WHERE status IN ('pending','retry')
		    AND next_attempt_at <= NOW()
		    AND (lease_expires_at IS NULL OR lease_expires_at < NOW())
		    AND ($1 <= 0 OR COALESCE(attempts,0) < $1)
		  ORDER BY next_attempt_at ASC, created_at ASC
		  FOR UPDATE SKIP LOCKED
		  LIMIT $2`,
		maxAttempts, limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var msgs []EmailOutboxMessage
	var ids []string
	for rows.Next() {
		var m EmailOutboxMessage
		var sentAt sql.NullTime
		if err := rows.Scan(
			&m.ID, &m.DedupeKey, &m.MessageType, &m.ToEmail, &m.Subject,
			&m.BodyText, &m.BodyHTML, &m.Status, &m.Attempts, &m.LastError,
			&m.NextAttempt, &m.CreatedAt, &sentAt,
		); err != nil {
			return nil, err
		}
		if sentAt.Valid {
			t := sentAt.Time
			m.SentAt = &t
		}
		msgs = append(msgs, m)
		ids = append(ids, m.ID)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(ids) == 0 {
		if err := tx.Commit(); err != nil {
			return nil, err
		}
		return nil, nil
	}

	if _, err := tx.Exec(
		`UPDATE email_outbox
		    SET status='sending',
		        attempts=COALESCE(attempts,0)+1,
		        last_error=NULL,
		        lease_owner=$1,
		        lease_acquired_at=NOW(),
		        lease_expires_at=NOW() + ($2 * INTERVAL '1 second')
		  WHERE id = ANY($3::uuid[])`,
		owner, leaseSeconds, pq.Array(ids),
	); err != nil {
		return nil, err
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}

	// Best-effort: reflect claim mutation on the returned items.
	for i := range msgs {
		msgs[i].Status = "sending"
		msgs[i].Attempts++
		msgs[i].LastError = ""
	}

	return msgs, nil
}

func MarkEmailOutboxSent(id string, owner string) error {
	_, err := database.DB.Exec(
		`UPDATE email_outbox
		    SET status='sent',
		        sent_at=NOW(),
		        lease_owner=NULL,
		        lease_acquired_at=NULL,
		        lease_expires_at=NULL
		  WHERE id=$1 AND lease_owner=$2`,
		strings.TrimSpace(id), strings.TrimSpace(owner),
	)
	return err
}

func MarkEmailOutboxRetry(id string, owner string, lastErr string, delay time.Duration) error {
	if delay <= 0 {
		delay = 30 * time.Second
	}
	_, err := database.DB.Exec(
		`UPDATE email_outbox
		    SET status='retry',
		        last_error=$3,
		        next_attempt_at=NOW() + ($4 * INTERVAL '1 second'),
		        lease_owner=NULL,
		        lease_acquired_at=NULL,
		        lease_expires_at=NULL
		  WHERE id=$1 AND lease_owner=$2`,
		strings.TrimSpace(id), strings.TrimSpace(owner), strings.TrimSpace(lastErr), int(delay.Seconds()),
	)
	return err
}

func MarkEmailOutboxDead(id string, owner string, lastErr string) error {
	_, err := database.DB.Exec(
		`UPDATE email_outbox
		    SET status='dead',
		        last_error=$3,
		        lease_owner=NULL,
		        lease_acquired_at=NULL,
		        lease_expires_at=NULL
		  WHERE id=$1 AND lease_owner=$2`,
		strings.TrimSpace(id), strings.TrimSpace(owner), strings.TrimSpace(lastErr),
	)
	return err
}

