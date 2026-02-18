package models

import (
	"database/sql"
	"time"

	"github.com/railpush/api/database"
)

type BillingCustomer struct {
	ID                   string    `json:"id"`
	UserID               string    `json:"user_id"`
	StripeCustomerID     string    `json:"stripe_customer_id"`
	StripeSubscriptionID string    `json:"stripe_subscription_id"`
	PaymentMethodLast4   string    `json:"payment_method_last4"`
	PaymentMethodBrand   string    `json:"payment_method_brand"`
	SubscriptionStatus   string    `json:"subscription_status"`
	CreditsMigrated      bool      `json:"credits_migrated"`
	LastBillingSyncAt    *time.Time `json:"last_billing_sync_at,omitempty"`
	CreatedAt            time.Time `json:"created_at"`
	UpdatedAt            time.Time `json:"updated_at"`
}

type BillingItem struct {
	ID                       string    `json:"id"`
	BillingCustomerID        string    `json:"billing_customer_id"`
	StripeSubscriptionItemID string    `json:"stripe_subscription_item_id"`
	StripePriceID            string    `json:"stripe_price_id"`
	ResourceType             string    `json:"resource_type"`
	ResourceID               string    `json:"resource_id"`
	ResourceName             string    `json:"resource_name"`
	Plan                     string    `json:"plan"`
	CreatedAt                time.Time `json:"created_at"`
	UpdatedAt                time.Time `json:"updated_at"`
}

func CreateBillingCustomer(bc *BillingCustomer) error {
	return database.DB.QueryRow(
		"INSERT INTO billing_customers (user_id, stripe_customer_id, stripe_subscription_id, subscription_status) VALUES ($1,$2,$3,$4) RETURNING id, created_at, updated_at",
		bc.UserID, bc.StripeCustomerID, bc.StripeSubscriptionID, bc.SubscriptionStatus,
	).Scan(&bc.ID, &bc.CreatedAt, &bc.UpdatedAt)
}

// UpsertBillingCustomer atomically inserts or retrieves a billing customer.
// If a concurrent request already inserted a row for this user_id, the ON CONFLICT
// clause returns the existing row instead of failing with a UNIQUE violation.
func UpsertBillingCustomer(userID, stripeCustomerID string) (*BillingCustomer, error) {
	bc := &BillingCustomer{}
	err := database.DB.QueryRow(
		`INSERT INTO billing_customers (user_id, stripe_customer_id, subscription_status)
		 VALUES ($1, $2, 'incomplete')
		 ON CONFLICT (user_id) DO UPDATE SET updated_at = NOW()
		 RETURNING id, user_id, stripe_customer_id, COALESCE(stripe_subscription_id,''),
		           COALESCE(payment_method_last4,''), COALESCE(payment_method_brand,''),
		           COALESCE(subscription_status,'incomplete'), COALESCE(credits_migrated,false),
		           created_at, updated_at`,
		userID, stripeCustomerID,
	).Scan(&bc.ID, &bc.UserID, &bc.StripeCustomerID, &bc.StripeSubscriptionID,
		&bc.PaymentMethodLast4, &bc.PaymentMethodBrand, &bc.SubscriptionStatus,
		&bc.CreditsMigrated, &bc.CreatedAt, &bc.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return bc, nil
}

func GetBillingCustomerByUserID(userID string) (*BillingCustomer, error) {
	bc := &BillingCustomer{}
	var lastSync sql.NullTime
	err := database.DB.QueryRow(
		"SELECT id, user_id, stripe_customer_id, COALESCE(stripe_subscription_id,''), COALESCE(payment_method_last4,''), COALESCE(payment_method_brand,''), COALESCE(subscription_status,'incomplete'), COALESCE(credits_migrated,false), last_billing_sync_at, created_at, updated_at FROM billing_customers WHERE user_id=$1",
		userID,
	).Scan(&bc.ID, &bc.UserID, &bc.StripeCustomerID, &bc.StripeSubscriptionID, &bc.PaymentMethodLast4, &bc.PaymentMethodBrand, &bc.SubscriptionStatus, &bc.CreditsMigrated, &lastSync, &bc.CreatedAt, &bc.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if lastSync.Valid {
		v := lastSync.Time
		bc.LastBillingSyncAt = &v
	}
	return bc, err
}

func GetBillingCustomerByStripeID(stripeCustomerID string) (*BillingCustomer, error) {
	bc := &BillingCustomer{}
	var lastSync sql.NullTime
	err := database.DB.QueryRow(
		"SELECT id, user_id, stripe_customer_id, COALESCE(stripe_subscription_id,''), COALESCE(payment_method_last4,''), COALESCE(payment_method_brand,''), COALESCE(subscription_status,'incomplete'), COALESCE(credits_migrated,false), last_billing_sync_at, created_at, updated_at FROM billing_customers WHERE stripe_customer_id=$1",
		stripeCustomerID,
	).Scan(&bc.ID, &bc.UserID, &bc.StripeCustomerID, &bc.StripeSubscriptionID, &bc.PaymentMethodLast4, &bc.PaymentMethodBrand, &bc.SubscriptionStatus, &bc.CreditsMigrated, &lastSync, &bc.CreatedAt, &bc.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if lastSync.Valid {
		v := lastSync.Time
		bc.LastBillingSyncAt = &v
	}
	return bc, err
}

func GetBillingCustomerByID(id string) (*BillingCustomer, error) {
	bc := &BillingCustomer{}
	var lastSync sql.NullTime
	err := database.DB.QueryRow(
		"SELECT id, user_id, stripe_customer_id, COALESCE(stripe_subscription_id,''), COALESCE(payment_method_last4,''), COALESCE(payment_method_brand,''), COALESCE(subscription_status,'incomplete'), COALESCE(credits_migrated,false), last_billing_sync_at, created_at, updated_at FROM billing_customers WHERE id=$1",
		id,
	).Scan(&bc.ID, &bc.UserID, &bc.StripeCustomerID, &bc.StripeSubscriptionID, &bc.PaymentMethodLast4, &bc.PaymentMethodBrand, &bc.SubscriptionStatus, &bc.CreditsMigrated, &lastSync, &bc.CreatedAt, &bc.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if lastSync.Valid {
		v := lastSync.Time
		bc.LastBillingSyncAt = &v
	}
	return bc, err
}

func UpdateBillingCustomer(bc *BillingCustomer) error {
	_, err := database.DB.Exec(
		"UPDATE billing_customers SET stripe_subscription_id=$1, payment_method_last4=$2, payment_method_brand=$3, subscription_status=$4, credits_migrated=$5, updated_at=NOW() WHERE id=$6",
		bc.StripeSubscriptionID, bc.PaymentMethodLast4, bc.PaymentMethodBrand, bc.SubscriptionStatus, bc.CreditsMigrated, bc.ID,
	)
	return err
}

func TouchBillingCustomerLastBillingSync(billingCustomerID string) error {
	_, err := database.DB.Exec(
		"UPDATE billing_customers SET last_billing_sync_at=NOW(), updated_at=NOW() WHERE id=$1",
		billingCustomerID,
	)
	return err
}

func CreateBillingItem(bi *BillingItem) error {
	return database.DB.QueryRow(
		"INSERT INTO billing_items (billing_customer_id, stripe_subscription_item_id, stripe_price_id, resource_type, resource_id, resource_name, plan) VALUES ($1,$2,$3,$4,$5,$6,$7) RETURNING id, created_at, updated_at",
		bi.BillingCustomerID, bi.StripeSubscriptionItemID, bi.StripePriceID, bi.ResourceType, bi.ResourceID, bi.ResourceName, bi.Plan,
	).Scan(&bi.ID, &bi.CreatedAt, &bi.UpdatedAt)
}

func GetBillingItemByResource(resourceType, resourceID string) (*BillingItem, error) {
	bi := &BillingItem{}
	err := database.DB.QueryRow(
		"SELECT id, billing_customer_id, stripe_subscription_item_id, stripe_price_id, resource_type, resource_id, COALESCE(resource_name,''), plan, created_at, updated_at FROM billing_items WHERE resource_type=$1 AND resource_id=$2",
		resourceType, resourceID,
	).Scan(&bi.ID, &bi.BillingCustomerID, &bi.StripeSubscriptionItemID, &bi.StripePriceID, &bi.ResourceType, &bi.ResourceID, &bi.ResourceName, &bi.Plan, &bi.CreatedAt, &bi.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return bi, err
}

func FindBillingSubscriptionItemIDByCustomerAndPrice(billingCustomerID, stripePriceID string) (string, error) {
	var id string
	err := database.DB.QueryRow(
		"SELECT COALESCE(stripe_subscription_item_id,'') FROM billing_items WHERE billing_customer_id=$1 AND stripe_price_id=$2 ORDER BY created_at DESC LIMIT 1",
		billingCustomerID, stripePriceID,
	).Scan(&id)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return id, err
}

func CountBillingItemsBySubscriptionItemID(stripeSubscriptionItemID string) (int, error) {
	var n int
	if err := database.DB.QueryRow(
		"SELECT COUNT(*) FROM billing_items WHERE stripe_subscription_item_id=$1",
		stripeSubscriptionItemID,
	).Scan(&n); err != nil {
		return 0, err
	}
	return n, nil
}

// CountBillingItemsBySubscriptionItemIDForUpdate counts billing items while holding
// a row-level lock (FOR UPDATE) to prevent concurrent quantity race conditions.
func CountBillingItemsBySubscriptionItemIDForUpdate(tx *sql.Tx, stripeSubscriptionItemID string) (int, error) {
	var n int
	if err := tx.QueryRow(
		"SELECT COUNT(*) FROM billing_items WHERE stripe_subscription_item_id=$1 FOR UPDATE",
		stripeSubscriptionItemID,
	).Scan(&n); err != nil {
		return 0, err
	}
	return n, nil
}

func ListBillingItemsByCustomer(billingCustomerID string) ([]BillingItem, error) {
	rows, err := database.DB.Query(
		"SELECT id, billing_customer_id, stripe_subscription_item_id, stripe_price_id, resource_type, resource_id, COALESCE(resource_name,''), plan, created_at, updated_at FROM billing_items WHERE billing_customer_id=$1 ORDER BY created_at DESC",
		billingCustomerID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var items []BillingItem
	for rows.Next() {
		var bi BillingItem
		if err := rows.Scan(&bi.ID, &bi.BillingCustomerID, &bi.StripeSubscriptionItemID, &bi.StripePriceID, &bi.ResourceType, &bi.ResourceID, &bi.ResourceName, &bi.Plan, &bi.CreatedAt, &bi.UpdatedAt); err != nil {
			return nil, err
		}
		items = append(items, bi)
	}
	return items, nil
}

// CountLegacyBillingItems returns how many billing_items are present without Stripe linkage.
// These represent legacy "credit-covered" rows and should be reconciled into Stripe.
func CountLegacyBillingItems(billingCustomerID string) (int, error) {
	var n int
	if err := database.DB.QueryRow(
		"SELECT COUNT(*) FROM billing_items WHERE billing_customer_id=$1 AND (COALESCE(stripe_subscription_item_id,'')='' OR COALESCE(stripe_price_id,'')='')",
		billingCustomerID,
	).Scan(&n); err != nil {
		return 0, err
	}
	return n, nil
}

func DeleteBillingItemByResource(resourceType, resourceID string) error {
	_, err := database.DB.Exec("DELETE FROM billing_items WHERE resource_type=$1 AND resource_id=$2", resourceType, resourceID)
	return err
}

func UpdateBillingItem(bi *BillingItem) error {
	_, err := database.DB.Exec(
		"UPDATE billing_items SET stripe_subscription_item_id=$1, stripe_price_id=$2, plan=$3, updated_at=NOW() WHERE id=$4",
		bi.StripeSubscriptionItemID, bi.StripePriceID, bi.Plan, bi.ID,
	)
	return err
}

func CountResourcesByWorkspaceAndPlan(workspaceID, resourceType, plan string) (int, error) {
	var count int
	var err error
	switch resourceType {
	case "service":
		err = database.DB.QueryRow("SELECT COUNT(*) FROM services WHERE workspace_id=$1 AND plan=$2", workspaceID, plan).Scan(&count)
	case "database":
		err = database.DB.QueryRow("SELECT COUNT(*) FROM managed_databases WHERE workspace_id=$1 AND plan=$2", workspaceID, plan).Scan(&count)
	case "keyvalue":
		err = database.DB.QueryRow("SELECT COUNT(*) FROM managed_keyvalue WHERE workspace_id=$1 AND plan=$2", workspaceID, plan).Scan(&count)
	}
	return count, err
}

// BillingInvoice represents a stored Stripe invoice for local reconciliation.
type BillingInvoice struct {
	ID                string     `json:"id"`
	BillingCustomerID string     `json:"billing_customer_id"`
	StripeInvoiceID   string     `json:"stripe_invoice_id"`
	Status            string     `json:"status"`
	AmountDueCents    int        `json:"amount_due_cents"`
	AmountPaidCents   int        `json:"amount_paid_cents"`
	Currency          string     `json:"currency"`
	HostedInvoiceURL  string     `json:"hosted_invoice_url,omitempty"`
	InvoicePDFURL     string     `json:"invoice_pdf_url,omitempty"`
	PeriodStart       *time.Time `json:"period_start,omitempty"`
	PeriodEnd         *time.Time `json:"period_end,omitempty"`
	CreatedAt         time.Time  `json:"created_at"`
}

func UpsertBillingInvoice(inv *BillingInvoice) error {
	return database.DB.QueryRow(
		`INSERT INTO billing_invoices (billing_customer_id, stripe_invoice_id, status, amount_due_cents, amount_paid_cents, currency, hosted_invoice_url, invoice_pdf_url, period_start, period_end)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)
		 ON CONFLICT (stripe_invoice_id) DO UPDATE SET status=$3, amount_due_cents=$4, amount_paid_cents=$5, hosted_invoice_url=$7, invoice_pdf_url=$8
		 RETURNING id, created_at`,
		inv.BillingCustomerID, inv.StripeInvoiceID, inv.Status, inv.AmountDueCents, inv.AmountPaidCents, inv.Currency, inv.HostedInvoiceURL, inv.InvoicePDFURL, inv.PeriodStart, inv.PeriodEnd,
	).Scan(&inv.ID, &inv.CreatedAt)
}

func ListBillingInvoicesByCustomer(billingCustomerID string, limit int) ([]BillingInvoice, error) {
	if limit <= 0 {
		limit = 20
	}
	rows, err := database.DB.Query(
		`SELECT id, billing_customer_id, stripe_invoice_id, status, amount_due_cents, amount_paid_cents, currency,
		        COALESCE(hosted_invoice_url,''), COALESCE(invoice_pdf_url,''), period_start, period_end, created_at
		   FROM billing_invoices WHERE billing_customer_id=$1 ORDER BY created_at DESC LIMIT $2`,
		billingCustomerID, limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []BillingInvoice
	for rows.Next() {
		var inv BillingInvoice
		var ps, pe sql.NullTime
		if err := rows.Scan(&inv.ID, &inv.BillingCustomerID, &inv.StripeInvoiceID, &inv.Status, &inv.AmountDueCents, &inv.AmountPaidCents, &inv.Currency, &inv.HostedInvoiceURL, &inv.InvoicePDFURL, &ps, &pe, &inv.CreatedAt); err != nil {
			continue
		}
		if ps.Valid {
			inv.PeriodStart = &ps.Time
		}
		if pe.Valid {
			inv.PeriodEnd = &pe.Time
		}
		out = append(out, inv)
	}
	if out == nil {
		out = []BillingInvoice{}
	}
	return out, nil
}
