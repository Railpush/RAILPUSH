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

func GetBillingCustomerByUserID(userID string) (*BillingCustomer, error) {
	bc := &BillingCustomer{}
	err := database.DB.QueryRow(
		"SELECT id, user_id, stripe_customer_id, COALESCE(stripe_subscription_id,''), COALESCE(payment_method_last4,''), COALESCE(payment_method_brand,''), COALESCE(subscription_status,'incomplete'), COALESCE(credits_migrated,false), created_at, updated_at FROM billing_customers WHERE user_id=$1",
		userID,
	).Scan(&bc.ID, &bc.UserID, &bc.StripeCustomerID, &bc.StripeSubscriptionID, &bc.PaymentMethodLast4, &bc.PaymentMethodBrand, &bc.SubscriptionStatus, &bc.CreditsMigrated, &bc.CreatedAt, &bc.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return bc, err
}

func GetBillingCustomerByStripeID(stripeCustomerID string) (*BillingCustomer, error) {
	bc := &BillingCustomer{}
	err := database.DB.QueryRow(
		"SELECT id, user_id, stripe_customer_id, COALESCE(stripe_subscription_id,''), COALESCE(payment_method_last4,''), COALESCE(payment_method_brand,''), COALESCE(subscription_status,'incomplete'), COALESCE(credits_migrated,false), created_at, updated_at FROM billing_customers WHERE stripe_customer_id=$1",
		stripeCustomerID,
	).Scan(&bc.ID, &bc.UserID, &bc.StripeCustomerID, &bc.StripeSubscriptionID, &bc.PaymentMethodLast4, &bc.PaymentMethodBrand, &bc.SubscriptionStatus, &bc.CreditsMigrated, &bc.CreatedAt, &bc.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return bc, err
}

func GetBillingCustomerByID(id string) (*BillingCustomer, error) {
	bc := &BillingCustomer{}
	err := database.DB.QueryRow(
		"SELECT id, user_id, stripe_customer_id, COALESCE(stripe_subscription_id,''), COALESCE(payment_method_last4,''), COALESCE(payment_method_brand,''), COALESCE(subscription_status,'incomplete'), COALESCE(credits_migrated,false), created_at, updated_at FROM billing_customers WHERE id=$1",
		id,
	).Scan(&bc.ID, &bc.UserID, &bc.StripeCustomerID, &bc.StripeSubscriptionID, &bc.PaymentMethodLast4, &bc.PaymentMethodBrand, &bc.SubscriptionStatus, &bc.CreditsMigrated, &bc.CreatedAt, &bc.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
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
