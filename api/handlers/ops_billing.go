package handlers

import (
	"net/http"
	"strings"
	"time"

	"github.com/gorilla/mux"
	"github.com/railpush/api/config"
	"github.com/railpush/api/database"
	"github.com/railpush/api/middleware"
	"github.com/railpush/api/services"
	"github.com/railpush/api/utils"
)

type OpsBillingHandler struct {
	Config *config.Config
}

func NewOpsBillingHandler(cfg *config.Config) *OpsBillingHandler {
	return &OpsBillingHandler{Config: cfg}
}

func (h *OpsBillingHandler) ensureOps(w http.ResponseWriter, r *http.Request) bool {
	userID := middleware.GetUserID(r)
	if err := services.EnsureOpsAccess(userID); err != nil {
		utils.RespondError(w, http.StatusForbidden, "forbidden")
		return false
	}
	return true
}

type opsBillingCustomerItem struct {
	ID                 string    `json:"id"`
	UserID             string    `json:"user_id"`
	Email              string    `json:"email"`
	Username           string    `json:"username"`
	StripeCustomerID   string    `json:"stripe_customer_id"`
	SubscriptionID     string    `json:"stripe_subscription_id"`
	SubscriptionStatus string    `json:"subscription_status"`
	PaymentBrand       string    `json:"payment_method_brand"`
	PaymentLast4       string    `json:"payment_method_last4"`
	ItemsCount         int64     `json:"items_count"`
	CreatedAt          time.Time `json:"created_at"`
	UpdatedAt          time.Time `json:"updated_at"`
}

type opsBillingItem struct {
	ID                       string    `json:"id"`
	ResourceType             string    `json:"resource_type"`
	ResourceID               string    `json:"resource_id"`
	ResourceName             string    `json:"resource_name"`
	Plan                     string    `json:"plan"`
	StripePriceID            string    `json:"stripe_price_id"`
	StripeSubscriptionItemID string    `json:"stripe_subscription_item_id"`
	CreatedAt                time.Time `json:"created_at"`
	UpdatedAt                time.Time `json:"updated_at"`
}

func (h *OpsBillingHandler) ListCustomers(w http.ResponseWriter, r *http.Request) {
	if !h.ensureOps(w, r) {
		return
	}
	limit := utils.GetQueryInt(r, "limit", 50)
	if limit < 1 {
		limit = 1
	}
	if limit > 200 {
		limit = 200
	}
	offset := utils.GetQueryInt(r, "offset", 0)
	if offset < 0 {
		offset = 0
	}
	status := strings.TrimSpace(r.URL.Query().Get("status"))
	q := strings.TrimSpace(r.URL.Query().Get("query"))
	like := "%" + q + "%"

	rows, err := database.DB.Query(
		`SELECT bc.id::text, COALESCE(bc.user_id::text,''), COALESCE(u.email,''), COALESCE(u.username,''),
		        COALESCE(bc.stripe_customer_id,''), COALESCE(bc.stripe_subscription_id,''), COALESCE(bc.subscription_status,''),
		        COALESCE(bc.payment_method_brand,''), COALESCE(bc.payment_method_last4,''),
		        (SELECT COUNT(*) FROM billing_items bi WHERE bi.billing_customer_id = bc.id) AS items_count,
		        bc.created_at, bc.updated_at
		   FROM billing_customers bc
		   LEFT JOIN users u ON u.id = bc.user_id
		  WHERE ($1 = '' OR COALESCE(bc.subscription_status,'') = $1)
		    AND ($2 = '' OR COALESCE(u.email,'') ILIKE $3 OR COALESCE(u.username,'') ILIKE $3 OR COALESCE(bc.stripe_customer_id,'') ILIKE $3)
		  ORDER BY bc.updated_at DESC, bc.created_at DESC
		  LIMIT $4 OFFSET $5`,
		status, q, like, limit, offset,
	)
	if err != nil {
		utils.RespondError(w, http.StatusInternalServerError, "failed to list billing customers")
		return
	}
	defer rows.Close()

	var out []opsBillingCustomerItem
	for rows.Next() {
		var it opsBillingCustomerItem
		if err := rows.Scan(
			&it.ID, &it.UserID, &it.Email, &it.Username,
			&it.StripeCustomerID, &it.SubscriptionID, &it.SubscriptionStatus,
			&it.PaymentBrand, &it.PaymentLast4, &it.ItemsCount,
			&it.CreatedAt, &it.UpdatedAt,
		); err != nil {
			continue
		}
		out = append(out, it)
	}
	if out == nil {
		out = []opsBillingCustomerItem{}
	}
	utils.RespondJSON(w, http.StatusOK, out)
}

func (h *OpsBillingHandler) GetCustomer(w http.ResponseWriter, r *http.Request) {
	if !h.ensureOps(w, r) {
		return
	}
	id := strings.TrimSpace(mux.Vars(r)["id"])

	var bc opsBillingCustomerItem
	err := database.DB.QueryRow(
		`SELECT bc.id::text, COALESCE(bc.user_id::text,''), COALESCE(u.email,''), COALESCE(u.username,''),
		        COALESCE(bc.stripe_customer_id,''), COALESCE(bc.stripe_subscription_id,''), COALESCE(bc.subscription_status,''),
		        COALESCE(bc.payment_method_brand,''), COALESCE(bc.payment_method_last4,''),
		        (SELECT COUNT(*) FROM billing_items bi WHERE bi.billing_customer_id = bc.id) AS items_count,
		        bc.created_at, bc.updated_at
		   FROM billing_customers bc
		   LEFT JOIN users u ON u.id = bc.user_id
		  WHERE bc.id=$1`,
		id,
	).Scan(
		&bc.ID, &bc.UserID, &bc.Email, &bc.Username,
		&bc.StripeCustomerID, &bc.SubscriptionID, &bc.SubscriptionStatus,
		&bc.PaymentBrand, &bc.PaymentLast4, &bc.ItemsCount,
		&bc.CreatedAt, &bc.UpdatedAt,
	)
	if err != nil {
		utils.RespondError(w, http.StatusNotFound, "billing customer not found")
		return
	}

	rows, err := database.DB.Query(
		`SELECT id::text, COALESCE(resource_type,''), COALESCE(resource_id::text,''), COALESCE(resource_name,''),
		        COALESCE(plan,''), COALESCE(stripe_price_id,''), COALESCE(stripe_subscription_item_id,''),
		        created_at, updated_at
		   FROM billing_items
		  WHERE billing_customer_id=$1
		  ORDER BY created_at DESC`,
		id,
	)
	if err != nil {
		utils.RespondError(w, http.StatusInternalServerError, "failed to load billing items")
		return
	}
	defer rows.Close()

	var items []opsBillingItem
	for rows.Next() {
		var it opsBillingItem
		if err := rows.Scan(&it.ID, &it.ResourceType, &it.ResourceID, &it.ResourceName, &it.Plan, &it.StripePriceID, &it.StripeSubscriptionItemID, &it.CreatedAt, &it.UpdatedAt); err != nil {
			continue
		}
		items = append(items, it)
	}
	if items == nil {
		items = []opsBillingItem{}
	}

	utils.RespondJSON(w, http.StatusOK, map[string]interface{}{
		"customer": bc,
		"items":    items,
	})
}

