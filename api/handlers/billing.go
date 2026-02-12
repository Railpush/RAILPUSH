package handlers

import (
	"encoding/json"
	"log"
	"net/http"
	"strings"

	"github.com/railpush/api/config"
	"github.com/railpush/api/middleware"
	"github.com/railpush/api/models"
	"github.com/railpush/api/services"
	"github.com/railpush/api/utils"
	"github.com/stripe/stripe-go/v81"
)

type BillingHandler struct {
	Config *config.Config
	Stripe *services.StripeService
}

func NewBillingHandler(cfg *config.Config, stripeService *services.StripeService) *BillingHandler {
	return &BillingHandler{Config: cfg, Stripe: stripeService}
}

func planRank(plan string) int {
	switch strings.ToLower(strings.TrimSpace(plan)) {
	case "pro":
		return 3
	case "standard":
		return 2
	case "starter":
		return 1
	default:
		return 0
	}
}

// GetBillingOverview returns the user's billing status, payment method, and all billing items.
func (h *BillingHandler) GetBillingOverview(w http.ResponseWriter, r *http.Request) {
	userID := middleware.GetUserID(r)
	bc, err := models.GetBillingCustomerByUserID(userID)
	if err != nil {
		utils.RespondError(w, http.StatusInternalServerError, "failed to get billing info")
		return
	}

	type BillingLineItem struct {
		ResourceType string `json:"resource_type"`
		ResourceID   string `json:"resource_id"`
		ResourceName string `json:"resource_name"`
		Plan         string `json:"plan"`
		MonthlyCost  int    `json:"monthly_cost"`
	}

	type BillingOverview struct {
		HasPaymentMethod   bool              `json:"has_payment_method"`
		PaymentMethodLast4 string            `json:"payment_method_last4"`
		PaymentMethodBrand string            `json:"payment_method_brand"`
		SubscriptionStatus string            `json:"subscription_status"`
		CurrentPlan        string            `json:"current_plan"`
		Items              []BillingLineItem `json:"items"`
		MonthlyTotal       int               `json:"monthly_total"`
	}

	overview := BillingOverview{
		Items: []BillingLineItem{},
	}

	var stripeSub *stripe.Subscription

	if bc != nil {
		if h.Stripe != nil && h.Stripe.Enabled() {
			if sub, syncErr := h.Stripe.SyncBillingCustomer(bc); syncErr != nil {
				log.Printf("Warning: failed to sync billing customer from Stripe: %v", syncErr)
			} else {
				stripeSub = sub
			}
		}

		overview.HasPaymentMethod = bc.PaymentMethodLast4 != ""
		overview.PaymentMethodLast4 = bc.PaymentMethodLast4
		overview.PaymentMethodBrand = bc.PaymentMethodBrand
		overview.SubscriptionStatus = bc.SubscriptionStatus

		items, err := models.ListBillingItemsByCustomer(bc.ID)
		if err == nil {
			for _, item := range items {
				cost := planCost(item.Plan)
				if planRank(item.Plan) > planRank(overview.CurrentPlan) {
					overview.CurrentPlan = item.Plan
				}
				overview.Items = append(overview.Items, BillingLineItem{
					ResourceType: item.ResourceType,
					ResourceID:   item.ResourceID,
					ResourceName: item.ResourceName,
					Plan:         item.Plan,
					MonthlyCost:  cost,
				})
				overview.MonthlyTotal += cost
			}
		}

		// If the user subscribed directly in Stripe (e.g. via customer portal), our DB might have no billing_items.
		// Fall back to Stripe subscription items so the dashboard stays accurate.
		if len(overview.Items) == 0 && stripeSub != nil && stripeSub.Items != nil {
			for _, si := range stripeSub.Items.Data {
				if si == nil || si.Price == nil {
					continue
				}
				plan := ""
				if h.Stripe != nil {
					plan = h.Stripe.PlanForPriceID(si.Price.ID)
				}

				monthly := 0
				if si.Price.UnitAmount > 0 {
					monthly = int(si.Price.UnitAmount)
				} else {
					monthly = planCost(plan)
				}
				qty := int64(1)
				if si.Quantity > 0 {
					qty = si.Quantity
				}
				monthly = monthly * int(qty)

				name := "Subscription"
				switch plan {
				case "starter":
					name = "RailPush Starter"
				case "standard":
					name = "RailPush Standard"
				case "pro":
					name = "RailPush Pro"
				}

				overview.Items = append(overview.Items, BillingLineItem{
					ResourceType: "subscription",
					ResourceID:   si.ID,
					ResourceName: name,
					Plan:         plan,
					MonthlyCost:  monthly,
				})
				overview.MonthlyTotal += monthly
				if planRank(plan) > planRank(overview.CurrentPlan) {
					overview.CurrentPlan = plan
				}
			}
		}
	}

	if strings.TrimSpace(overview.CurrentPlan) == "" {
		overview.CurrentPlan = "free"
	}

	utils.RespondJSON(w, http.StatusOK, overview)
}

// CreateCheckoutSession creates a Stripe Checkout session to collect payment method.
func (h *BillingHandler) CreateCheckoutSession(w http.ResponseWriter, r *http.Request) {
	if !h.Stripe.Enabled() {
		utils.RespondError(w, http.StatusServiceUnavailable, "billing not configured")
		return
	}

	userID := middleware.GetUserID(r)
	user, err := models.GetUserByID(userID)
	if err != nil || user == nil {
		utils.RespondError(w, http.StatusInternalServerError, "failed to get user")
		return
	}

	bc, err := h.Stripe.EnsureCustomer(userID, user.Email)
	if err != nil {
		utils.RespondError(w, http.StatusInternalServerError, "failed to create billing customer: "+err.Error())
		return
	}

	var body struct {
		ReturnURL string `json:"return_url"`
	}
	json.NewDecoder(r.Body).Decode(&body)
	if body.ReturnURL == "" {
		body.ReturnURL = "https://" + h.Config.Deploy.Domain + "/billing"
	}

	url, err := h.Stripe.CreateCheckoutSession(bc.StripeCustomerID, body.ReturnURL)
	if err != nil {
		utils.RespondError(w, http.StatusInternalServerError, "failed to create checkout session: "+err.Error())
		return
	}

	utils.RespondJSON(w, http.StatusOK, map[string]string{"url": url})
}

// GetPaymentMethod returns the current payment method on file.
func (h *BillingHandler) GetPaymentMethod(w http.ResponseWriter, r *http.Request) {
	userID := middleware.GetUserID(r)
	bc, err := models.GetBillingCustomerByUserID(userID)
	if err != nil {
		utils.RespondError(w, http.StatusInternalServerError, "failed to get billing info")
		return
	}

	if bc == nil || bc.PaymentMethodLast4 == "" {
		utils.RespondJSON(w, http.StatusOK, map[string]interface{}{
			"has_payment_method": false,
		})
		return
	}

	utils.RespondJSON(w, http.StatusOK, map[string]interface{}{
		"has_payment_method": true,
		"last4":              bc.PaymentMethodLast4,
		"brand":              bc.PaymentMethodBrand,
	})
}

// CreatePortalSession creates a Stripe Customer Portal session.
func (h *BillingHandler) CreatePortalSession(w http.ResponseWriter, r *http.Request) {
	if !h.Stripe.Enabled() {
		utils.RespondError(w, http.StatusServiceUnavailable, "billing not configured")
		return
	}

	userID := middleware.GetUserID(r)
	bc, err := models.GetBillingCustomerByUserID(userID)
	if err != nil || bc == nil {
		utils.RespondError(w, http.StatusBadRequest, "no billing account found")
		return
	}

	var body struct {
		ReturnURL string `json:"return_url"`
	}
	json.NewDecoder(r.Body).Decode(&body)
	if body.ReturnURL == "" {
		body.ReturnURL = "https://" + h.Config.Deploy.Domain + "/billing"
	}

	url, err := h.Stripe.CreatePortalSession(bc.StripeCustomerID, body.ReturnURL)
	if err != nil {
		utils.RespondError(w, http.StatusInternalServerError, "failed to create portal session: "+err.Error())
		return
	}

	utils.RespondJSON(w, http.StatusOK, map[string]string{"url": url})
}

// StripeWebhook handles incoming Stripe webhook events.
func (h *BillingHandler) StripeWebhook(w http.ResponseWriter, r *http.Request) {
	payload, err := services.ReadBody(r.Body)
	if err != nil {
		utils.RespondError(w, http.StatusBadRequest, "failed to read body")
		return
	}

	sig := r.Header.Get("Stripe-Signature")
	if err := h.Stripe.HandleWebhookEvent(payload, sig); err != nil {
		utils.RespondError(w, http.StatusBadRequest, err.Error())
		return
	}

	utils.RespondJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func planCost(plan string) int {
	switch plan {
	case "starter":
		return 700
	case "standard":
		return 2500
	case "pro":
		return 8500
	default:
		return 0
	}
}
