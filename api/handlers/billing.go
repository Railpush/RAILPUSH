package handlers

import (
	"encoding/json"
	"errors"
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
		ResourceType  string `json:"resource_type"`
		ResourceID    string `json:"resource_id"`
		ResourceName  string `json:"resource_name"`
		Plan          string `json:"plan"`
		MonthlyCost   int    `json:"monthly_cost"`
		CreditCovered bool   `json:"credit_covered"`
	}

	type BillingOverview struct {
		HasPaymentMethod   bool              `json:"has_payment_method"`
		PaymentMethodLast4 string            `json:"payment_method_last4"`
		PaymentMethodBrand string            `json:"payment_method_brand"`
		SubscriptionStatus string            `json:"subscription_status"`
		CurrentPlan        string            `json:"current_plan"`
		Items              []BillingLineItem `json:"items"`
		MonthlyTotal       int               `json:"monthly_total"`
		CreditCoveredTotal int               `json:"credit_covered_total"`
		CreditBalanceCents int64             `json:"credit_balance_cents"`
		NextInvoiceTotalCents         int64 `json:"next_invoice_total_cents"`
		NextInvoiceAmountDueCents     int64 `json:"next_invoice_amount_due_cents"`
		NextInvoiceCreditAppliedCents int64 `json:"next_invoice_credit_applied_cents"`
		NextInvoiceCreditCarryCents   int64 `json:"next_invoice_credit_carry_cents"`
	}

	overview := BillingOverview{
		Items: []BillingLineItem{},
	}

	var stripeSub *stripe.Subscription

	workspaceID := ""
	if ws, _ := models.GetWorkspaceByOwner(userID); ws != nil && strings.TrimSpace(ws.ID) != "" {
		workspaceID = ws.ID
		if bal, err := models.GetWorkspaceCreditBalanceCents(ws.ID); err != nil {
			log.Printf("Warning: failed to load workspace credit balance: ws=%s err=%v", ws.ID, err)
		} else {
			overview.CreditBalanceCents = bal
		}
	}

	if bc != nil {
		if h.Stripe != nil && h.Stripe.Enabled() {
			// One-time migration so existing workspace credits become Stripe customer balance.
			if workspaceID != "" {
				if err := h.Stripe.EnsureWorkspaceCreditsMigrated(bc, workspaceID); err != nil {
					log.Printf("Warning: failed to migrate workspace credits to Stripe: %v", err)
				}
			}

			if sub, syncErr := h.Stripe.SyncBillingCustomer(bc); syncErr != nil {
				log.Printf("Warning: failed to sync billing customer from Stripe: %v", syncErr)
			} else {
				stripeSub = sub
			}

			// Use Stripe as source of truth for invoice credits and the next charge amount.
			if credit, err := h.Stripe.CreditBalanceCents(bc.StripeCustomerID); err != nil {
				log.Printf("Warning: failed to fetch Stripe credit balance: %v", err)
			} else {
				overview.CreditBalanceCents = credit
			}
			if overview.CreditBalanceCents < 0 {
				overview.CreditBalanceCents = 0
			}

			if inv, err := h.Stripe.UpcomingInvoice(bc.StripeCustomerID, bc.StripeSubscriptionID); err != nil {
				log.Printf("Warning: failed to fetch Stripe upcoming invoice: %v", err)
			} else if inv != nil {
				overview.NextInvoiceTotalCents = inv.Total
				overview.NextInvoiceAmountDueCents = inv.AmountDue
				if inv.Total > inv.AmountDue {
					overview.NextInvoiceCreditAppliedCents = inv.Total - inv.AmountDue
				}
				if overview.CreditBalanceCents > 0 && overview.NextInvoiceCreditAppliedCents > 0 {
					if overview.CreditBalanceCents > overview.NextInvoiceCreditAppliedCents {
						overview.NextInvoiceCreditCarryCents = overview.CreditBalanceCents - overview.NextInvoiceCreditAppliedCents
					}
				}
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
				// Items with empty Stripe IDs were paid for by credits — they are not unbilled.
				creditCovered := strings.TrimSpace(item.StripeSubscriptionItemID) == "" && strings.TrimSpace(item.StripePriceID) == ""
				overview.Items = append(overview.Items, BillingLineItem{
					ResourceType:  item.ResourceType,
					ResourceID:    item.ResourceID,
					ResourceName:  item.ResourceName,
					Plan:          item.Plan,
					MonthlyCost:   cost,
					CreditCovered: creditCovered,
				})
				if creditCovered {
					overview.CreditCoveredTotal += cost
				} else {
					overview.MonthlyTotal += cost
				}
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
		body.ReturnURL = "https://" + h.Config.ControlPlane.Domain + "/billing"
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
		body.ReturnURL = "https://" + h.Config.ControlPlane.Domain + "/billing"
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

	if h.Stripe == nil || !h.Stripe.WebhookEnabled() {
		log.Printf("stripe webhook: billing not configured (missing STRIPE_SECRET_KEY / STRIPE_WEBHOOK_SECRET)")
		utils.RespondError(w, http.StatusServiceUnavailable, "billing not configured")
		return
	}

	sig := r.Header.Get("Stripe-Signature")
	if strings.TrimSpace(sig) == "" {
		utils.RespondError(w, http.StatusBadRequest, "missing stripe signature")
		return
	}

	if err := h.Stripe.HandleWebhookEvent(payload, sig); err != nil {
		// Signature verification failures are a client/config error (Stripe will not retry).
		if errors.Is(err, services.ErrStripeWebhookSignature) {
			log.Printf("stripe webhook: invalid signature: %v", err)
			utils.RespondError(w, http.StatusBadRequest, "invalid stripe signature")
			return
		}
		// Processing errors should be retried by Stripe.
		log.Printf("stripe webhook: handler error: %v", err)
		utils.RespondError(w, http.StatusInternalServerError, "webhook processing failed")
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
