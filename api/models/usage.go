package models

import (
	"database/sql"
	"time"

	"github.com/railpush/api/database"
)

// ResourceUsageEvent records when a resource started or stopped running.
// Events: "start" (created/resumed), "stop" (suspended/deleted).
type ResourceUsageEvent struct {
	ID           string    `json:"id"`
	ResourceType string    `json:"resource_type"`
	ResourceID   string    `json:"resource_id"`
	Event        string    `json:"event"` // "start" or "stop"
	OccurredAt   time.Time `json:"occurred_at"`
}

// RecordUsageEvent inserts a start or stop event for a resource.
func RecordUsageEvent(resourceType, resourceID, event string) error {
	_, err := database.DB.Exec(
		"INSERT INTO resource_usage_events (resource_type, resource_id, event) VALUES ($1, $2, $3)",
		resourceType, resourceID, event,
	)
	return err
}

// RecordUsageEventAt inserts a usage event with a specific timestamp.
func RecordUsageEventAt(resourceType, resourceID, event string, at time.Time) error {
	_, err := database.DB.Exec(
		"INSERT INTO resource_usage_events (resource_type, resource_id, event, occurred_at) VALUES ($1, $2, $3, $4)",
		resourceType, resourceID, event, at,
	)
	return err
}

// GetLatestUsageEvent returns the most recent usage event for a resource.
func GetLatestUsageEvent(resourceType, resourceID string) (*ResourceUsageEvent, error) {
	e := &ResourceUsageEvent{}
	err := database.DB.QueryRow(
		"SELECT id, resource_type, resource_id, event, occurred_at FROM resource_usage_events WHERE resource_type=$1 AND resource_id=$2 ORDER BY occurred_at DESC LIMIT 1",
		resourceType, resourceID,
	).Scan(&e.ID, &e.ResourceType, &e.ResourceID, &e.Event, &e.OccurredAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return e, err
}

// IsResourceActive returns true if the latest usage event for the resource is "start".
func IsResourceActive(resourceType, resourceID string) bool {
	e, err := GetLatestUsageEvent(resourceType, resourceID)
	if err != nil || e == nil {
		return false
	}
	return e.Event == "start"
}

// CalcActiveMinutesSince calculates the number of active minutes for a resource
// between `since` and `now` by walking through usage events in chronological order.
func CalcActiveMinutesSince(resourceType, resourceID string, since, now time.Time) (int64, error) {
	rows, err := database.DB.Query(
		`SELECT event, occurred_at FROM resource_usage_events
		 WHERE resource_type=$1 AND resource_id=$2 AND occurred_at >= $3
		 ORDER BY occurred_at ASC`,
		resourceType, resourceID, since,
	)
	if err != nil {
		return 0, err
	}
	defer rows.Close()

	// We need to know the state at `since`. Check the last event before `since`.
	var priorEvent string
	_ = database.DB.QueryRow(
		`SELECT event FROM resource_usage_events
		 WHERE resource_type=$1 AND resource_id=$2 AND occurred_at < $3
		 ORDER BY occurred_at DESC LIMIT 1`,
		resourceType, resourceID, since,
	).Scan(&priorEvent)

	active := priorEvent == "start"
	cursor := since
	var totalMinutes int64

	for rows.Next() {
		var evt string
		var at time.Time
		if err := rows.Scan(&evt, &at); err != nil {
			continue
		}
		if at.After(now) {
			at = now
		}
		if active && at.After(cursor) {
			totalMinutes += int64(at.Sub(cursor).Minutes())
		}
		active = evt == "start"
		cursor = at
	}

	// If still active at `now`, count remaining minutes.
	if active && now.After(cursor) {
		totalMinutes += int64(now.Sub(cursor).Minutes())
	}

	return totalMinutes, nil
}

// ActiveMeteredBillingItem is a billing_item that has metered tracking enabled.
type ActiveMeteredBillingItem struct {
	BillingItem
	LastUsageReportedAt *time.Time
}

// ListActiveMeteredBillingItems returns all billing items with is_metered=true
// that have an active resource (latest usage event is "start").
func ListActiveMeteredBillingItems() ([]ActiveMeteredBillingItem, error) {
	rows, err := database.DB.Query(
		`SELECT bi.id, bi.billing_customer_id, bi.stripe_subscription_item_id, bi.stripe_price_id,
		        bi.resource_type, bi.resource_id, COALESCE(bi.resource_name,''), bi.plan,
		        bi.created_at, bi.updated_at, bi.last_usage_reported_at
		   FROM billing_items bi
		  WHERE bi.is_metered = TRUE
		    AND bi.stripe_subscription_item_id != ''`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var items []ActiveMeteredBillingItem
	for rows.Next() {
		var item ActiveMeteredBillingItem
		var lastReported sql.NullTime
		if err := rows.Scan(&item.ID, &item.BillingCustomerID, &item.StripeSubscriptionItemID,
			&item.StripePriceID, &item.ResourceType, &item.ResourceID, &item.ResourceName,
			&item.Plan, &item.CreatedAt, &item.UpdatedAt, &lastReported); err != nil {
			continue
		}
		if lastReported.Valid {
			item.LastUsageReportedAt = &lastReported.Time
		}
		items = append(items, item)
	}
	return items, nil
}

// UpdateBillingItemLastUsageReported updates the last_usage_reported_at timestamp.
func UpdateBillingItemLastUsageReported(billingItemID string, at time.Time) error {
	_, err := database.DB.Exec(
		"UPDATE billing_items SET last_usage_reported_at=$1, updated_at=NOW() WHERE id=$2",
		at, billingItemID,
	)
	return err
}
