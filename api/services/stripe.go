package services

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/railpush/api/config"
	"github.com/railpush/api/database"
	"github.com/railpush/api/models"
	"github.com/stripe/stripe-go/v81"
	billingportalsession "github.com/stripe/stripe-go/v81/billingportal/session"
	"github.com/stripe/stripe-go/v81/checkout/session"
	customerbalancetransaction "github.com/stripe/stripe-go/v81/customerbalancetransaction"
	"github.com/stripe/stripe-go/v81/customer"
	"github.com/stripe/stripe-go/v81/paymentmethod"
	"github.com/stripe/stripe-go/v81/setupintent"
	"github.com/stripe/stripe-go/v81/subscription"
	"github.com/stripe/stripe-go/v81/subscriptionitem"
	"github.com/stripe/stripe-go/v81/usagerecord"
	"github.com/stripe/stripe-go/v81/webhook"
)

type StripeService struct {
	Config *config.Config
}

var ErrNoDefaultPaymentMethod = errors.New("payment method required")
var ErrStripeWebhookSignature = errors.New("stripe webhook signature verification failed")

var stripeIDTokenRE = regexp.MustCompile(`\b(?:price|si)_[A-Za-z0-9]+\b`)
var stripeURLTokenRE = regexp.MustCompile(`https?://\S+`)

// InvoicePreview is the subset of Stripe invoice fields we need for "next invoice" display.
// It is returned by Stripe's "create preview invoice" API.
type InvoicePreview struct {
	Total              int64  `json:"total"`
	AmountDue          int64  `json:"amount_due"`
	PeriodEnd          int64  `json:"period_end"`
	NextPaymentAttempt *int64 `json:"next_payment_attempt"`
	DueDate            *int64 `json:"due_date"`
}

func NewStripeService(cfg *config.Config) *StripeService {
	stripe.Key = cfg.Stripe.SecretKey
	svc := &StripeService{Config: cfg}
	svc.validateConfig()
	return svc
}

// validateConfig logs warnings on startup if Stripe is enabled but price IDs are missing.
func (s *StripeService) validateConfig() {
	if !s.Enabled() {
		return
	}
	missing := []string{}
	if strings.TrimSpace(s.Config.Stripe.PriceStarter) == "" {
		missing = append(missing, "STRIPE_PRICE_STARTER")
	}
	if strings.TrimSpace(s.Config.Stripe.PriceStandard) == "" {
		missing = append(missing, "STRIPE_PRICE_STANDARD")
	}
	if strings.TrimSpace(s.Config.Stripe.PricePro) == "" {
		missing = append(missing, "STRIPE_PRICE_PRO")
	}
	if len(missing) > 0 {
		log.Printf("WARNING: Stripe is enabled but missing price IDs: %s — paid plan upgrades will fail", strings.Join(missing, ", "))
	}
	if strings.TrimSpace(s.Config.Stripe.WebhookSecret) == "" {
		log.Printf("WARNING: Stripe is enabled but STRIPE_WEBHOOK_SECRET is not set — webhooks will be rejected")
	}
}

func (s *StripeService) Enabled() bool {
	return s.Config.Stripe.SecretKey != ""
}

func (s *StripeService) WebhookEnabled() bool {
	return s != nil && s.Enabled() && strings.TrimSpace(s.Config.Stripe.WebhookSecret) != ""
}

func (s *StripeService) PriceIDForPlan(plan string) string {
	switch plan {
	case "starter":
		return s.Config.Stripe.PriceStarter
	case "standard":
		return s.Config.Stripe.PriceStandard
	case "pro":
		return s.Config.Stripe.PricePro
	default:
		return ""
	}
}

func (s *StripeService) PlanForPriceID(priceID string) string {
	priceID = strings.TrimSpace(priceID)
	if priceID == "" || s == nil || s.Config == nil {
		return ""
	}
	switch priceID {
	case strings.TrimSpace(s.Config.Stripe.PriceStarter):
		return "starter"
	case strings.TrimSpace(s.Config.Stripe.PriceStandard):
		return "standard"
	case strings.TrimSpace(s.Config.Stripe.PricePro):
		return "pro"
	default:
		return ""
	}
}

// MeteredBillingEnabled returns true when at least one metered price ID is configured.
// When enabled, new resources use per-minute metered subscription items instead of flat-rate.
func (s *StripeService) MeteredBillingEnabled() bool {
	if s == nil || s.Config == nil {
		return false
	}
	return strings.TrimSpace(s.Config.Stripe.MeteredPriceStarter) != "" ||
		strings.TrimSpace(s.Config.Stripe.MeteredPriceStandard) != "" ||
		strings.TrimSpace(s.Config.Stripe.MeteredPricePro) != ""
}

// MeteredPriceIDForPlan returns the metered Stripe price ID for the given plan.
// Returns empty string if metered billing is not configured for that plan.
func (s *StripeService) MeteredPriceIDForPlan(plan string) string {
	if s == nil || s.Config == nil {
		return ""
	}
	switch plan {
	case "starter":
		return strings.TrimSpace(s.Config.Stripe.MeteredPriceStarter)
	case "standard":
		return strings.TrimSpace(s.Config.Stripe.MeteredPriceStandard)
	case "pro":
		return strings.TrimSpace(s.Config.Stripe.MeteredPricePro)
	default:
		return ""
	}
}

// ReportUsageMinutes reports usage (in minutes) to Stripe for a metered subscription item.
// This is called hourly by the scheduler for active resources.
func (s *StripeService) ReportUsageMinutes(subscriptionItemID string, minutes int64, timestamp time.Time) error {
	if minutes <= 0 {
		return nil
	}
	params := &stripe.UsageRecordParams{
		SubscriptionItem: stripe.String(subscriptionItemID),
		Quantity:         stripe.Int64(minutes),
		Timestamp:        stripe.Int64(timestamp.Unix()),
		Action:           stripe.String("increment"),
	}
	_, err := usagerecord.New(params)
	if err != nil {
		log.Printf("Stripe: report usage failed sub_item=%s minutes=%d err=%v", subscriptionItemID, minutes, err)
		return fmt.Errorf("usage report failed: %w", err)
	}
	return nil
}

func sanitizeStripeMessage(msg string) string {
	msg = strings.TrimSpace(msg)
	if msg == "" {
		return ""
	}
	msg = stripeURLTokenRE.ReplaceAllString(msg, "")
	msg = stripeIDTokenRE.ReplaceAllStringFunc(msg, func(tok string) string {
		if strings.HasPrefix(tok, "price_") {
			return "selected plan"
		}
		if strings.HasPrefix(tok, "si_") {
			return "existing subscription item"
		}
		return tok
	})
	msg = strings.Join(strings.Fields(msg), " ")
	return strings.TrimSpace(msg)
}

func stripeUserError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, ErrNoDefaultPaymentMethod) {
		return ErrNoDefaultPaymentMethod
	}
	var se *stripe.Error
	if errors.As(err, &se) {
		// Normalize common "missing card" Stripe errors into a consistent sentinel so
		// the UI can show the upgrade/payment-method flow.
		raw := strings.ToLower(strings.TrimSpace(se.Msg))
		if raw != "" {
			if strings.Contains(raw, "no attached payment source") ||
				strings.Contains(raw, "no payment method") ||
				strings.Contains(raw, "default payment method") ||
				strings.Contains(raw, "payment method is required") ||
				strings.Contains(raw, "cannot charge a customer that has no active card") {
				return ErrNoDefaultPaymentMethod
			}
		}
		if msg := sanitizeStripeMessage(se.Msg); msg != "" {
			return fmt.Errorf("%s", msg)
		}
	}
	if msg := sanitizeStripeMessage(err.Error()); msg != "" {
		raw := strings.ToLower(msg)
		if strings.Contains(raw, "no attached payment source") || strings.Contains(raw, "payment method") && strings.Contains(raw, "required") {
			return ErrNoDefaultPaymentMethod
		}
		return fmt.Errorf("%s", msg)
	}
	return fmt.Errorf("billing update failed. please try again or contact support")
}

// EnsureCustomer creates or retrieves a Stripe customer for the given user.
// Uses ON CONFLICT to safely handle concurrent requests for the same user.
func (s *StripeService) EnsureCustomer(userID, email string) (*models.BillingCustomer, error) {
	bc, err := models.GetBillingCustomerByUserID(userID)
	if err != nil {
		log.Printf("Stripe: failed to query billing customer user=%s err=%v", userID, err)
		return nil, fmt.Errorf("billing update failed. please try again")
	}
	if bc != nil {
		return bc, nil
	}

	params := &stripe.CustomerParams{
		Email: stripe.String(email),
	}
	params.AddMetadata("user_id", userID)
	cust, err := customer.New(params)
	if err != nil {
		log.Printf("Stripe: failed to create customer user=%s err=%v", userID, err)
		return nil, stripeUserError(err)
	}

	bc, err = models.UpsertBillingCustomer(userID, cust.ID)
	if err != nil {
		log.Printf("Stripe: failed to save billing customer user=%s stripe_customer=%s err=%v", userID, cust.ID, err)
		return nil, fmt.Errorf("billing update failed. please try again")
	}
	return bc, nil
}

func subscriptionStatusScore(status string) int {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "active":
		return 6
	case "trialing":
		return 5
	case "past_due":
		return 4
	case "unpaid":
		return 3
	case "incomplete":
		return 2
	case "incomplete_expired":
		return 1
	case "ended":
		return 0
	case "canceled", "cancelled":
		return -1
	default:
		return 0
	}
}

func (s *StripeService) fetchBestSubscription(stripeCustomerID, preferredSubscriptionID string) (*stripe.Subscription, error) {
	stripeCustomerID = strings.TrimSpace(stripeCustomerID)
	if stripeCustomerID == "" {
		return nil, nil
	}

	preferredSubscriptionID = strings.TrimSpace(preferredSubscriptionID)
	if preferredSubscriptionID != "" {
		getParams := &stripe.SubscriptionParams{}
		getParams.AddExpand("default_payment_method")
		getParams.AddExpand("items.data.price")
		sub, err := subscription.Get(preferredSubscriptionID, getParams)
		if err == nil && sub != nil {
			return sub, nil
		}
	}

	listParams := &stripe.SubscriptionListParams{
		Customer: stripe.String(stripeCustomerID),
		Status:   stripe.String("all"),
	}
	listParams.Limit = stripe.Int64(10)
	listParams.AddExpand("data.default_payment_method")
	listParams.AddExpand("data.items.data.price")

	iter := subscription.List(listParams)

	var best *stripe.Subscription
	bestScore := -999
	var bestCreated int64

	for iter.Next() {
		sub := iter.Subscription()
		if sub == nil {
			continue
		}
		score := subscriptionStatusScore(string(sub.Status))
		if best == nil || score > bestScore || (score == bestScore && sub.Created > bestCreated) {
			best = sub
			bestScore = score
			bestCreated = sub.Created
		}
	}
	if err := iter.Err(); err != nil {
		return nil, fmt.Errorf("failed to list subscriptions: %w", err)
	}
	return best, nil
}

// SyncBillingCustomer attempts to refresh subscription/payment method fields from Stripe so
// the dashboard reflects Stripe's source of truth even if webhooks were missed.
func (s *StripeService) SyncBillingCustomer(bc *models.BillingCustomer) (*stripe.Subscription, error) {
	if s == nil || !s.Enabled() || bc == nil || strings.TrimSpace(bc.StripeCustomerID) == "" {
		return nil, nil
	}

	// Best-effort: sync default payment method from Stripe customer.
	_, _ = s.getDefaultPaymentMethod(bc)

	sub, err := s.fetchBestSubscription(bc.StripeCustomerID, bc.StripeSubscriptionID)
	if err != nil {
		return nil, err
	}
	if sub == nil {
		return nil, nil
	}

	changed := false
	if bc.StripeSubscriptionID != sub.ID {
		bc.StripeSubscriptionID = sub.ID
		changed = true
	}
	subStatus := string(sub.Status)
	if strings.TrimSpace(subStatus) != "" && bc.SubscriptionStatus != subStatus {
		bc.SubscriptionStatus = subStatus
		changed = true
	}
	if sub.DefaultPaymentMethod != nil && sub.DefaultPaymentMethod.Card != nil {
		last4 := strings.TrimSpace(sub.DefaultPaymentMethod.Card.Last4)
		brand := strings.TrimSpace(string(sub.DefaultPaymentMethod.Card.Brand))
		if last4 != "" && (bc.PaymentMethodLast4 != last4 || bc.PaymentMethodBrand != brand) {
			bc.PaymentMethodLast4 = last4
			bc.PaymentMethodBrand = brand
			changed = true
		}
	}

	if changed {
		if err := models.UpdateBillingCustomer(bc); err != nil {
			return sub, err
		}
	}
	return sub, nil
}

// CreateCheckoutSession creates a Stripe Checkout session in setup mode to collect a payment method.
func (s *StripeService) CreateCheckoutSession(stripeCustomerID, returnURL string) (string, error) {
	params := &stripe.CheckoutSessionParams{
		Customer:           stripe.String(stripeCustomerID),
		Mode:               stripe.String(string(stripe.CheckoutSessionModeSetup)),
		PaymentMethodTypes: stripe.StringSlice([]string{"card"}),
		SuccessURL:         stripe.String(returnURL + "?billing=success"),
		CancelURL:          stripe.String(returnURL + "?billing=cancel"),
	}
	sess, err := session.New(params)
	if err != nil {
		log.Printf("Stripe: failed to create checkout session customer=%s err=%v", stripeCustomerID, err)
		return "", stripeUserError(err)
	}
	return sess.URL, nil
}

// CreatePortalSession creates a Stripe Customer Portal session.
func (s *StripeService) CreatePortalSession(stripeCustomerID, returnURL string) (string, error) {
	params := &stripe.BillingPortalSessionParams{
		Customer:  stripe.String(stripeCustomerID),
		ReturnURL: stripe.String(returnURL),
	}
	sess, err := billingportalsession.New(params)
	if err != nil {
		log.Printf("Stripe: failed to create portal session customer=%s err=%v", stripeCustomerID, err)
		return "", stripeUserError(err)
	}
	return sess.URL, nil
}

func (s *StripeService) getDefaultPaymentMethod(bc *models.BillingCustomer) (string, error) {
	params := &stripe.CustomerParams{}
	params.AddExpand("invoice_settings.default_payment_method")

	cust, err := customer.Get(bc.StripeCustomerID, params)
	if err != nil {
		return "", fmt.Errorf("failed to fetch Stripe customer: %w", err)
	}
	if cust == nil || cust.InvoiceSettings == nil || cust.InvoiceSettings.DefaultPaymentMethod == nil {
		return "", nil
	}

	pm := cust.InvoiceSettings.DefaultPaymentMethod
	if pm.Card != nil {
		bc.PaymentMethodLast4 = pm.Card.Last4
		bc.PaymentMethodBrand = string(pm.Card.Brand)
		if err := models.UpdateBillingCustomer(bc); err != nil {
			log.Printf("Warning: failed to sync payment method for customer %s: %v", bc.ID, err)
		}
	}

	return pm.ID, nil
}

func (s *StripeService) createCustomerBalanceTransaction(stripeCustomerID string, amountCents int64, description string, metadata map[string]string) error {
	if s == nil || !s.Enabled() {
		return nil
	}
	stripeCustomerID = strings.TrimSpace(stripeCustomerID)
	if stripeCustomerID == "" || amountCents == 0 {
		return nil
	}

	params := &stripe.CustomerBalanceTransactionParams{
		Customer: stripe.String(stripeCustomerID),
		Amount:   stripe.Int64(amountCents),
		Currency: stripe.String("usd"),
	}
	if desc := strings.TrimSpace(description); desc != "" {
		params.Description = stripe.String(desc)
	}
	for k, v := range metadata {
		k = strings.TrimSpace(k)
		v = strings.TrimSpace(v)
		if k == "" || v == "" {
			continue
		}
		params.AddMetadata(k, v)
	}

	_, err := customerbalancetransaction.New(params)
	return err
}

// ApplyWorkspaceCreditDelta mirrors a workspace credit ledger adjustment into Stripe customer balance.
//
// Convention: workspace credits are stored as positive cents. Stripe customer balance uses negative
// values to represent credits applied to future invoices, so we invert the sign.
func (s *StripeService) ApplyWorkspaceCreditDelta(stripeCustomerID, workspaceID string, amountCents int64, reason string) error {
	if s == nil || !s.Enabled() {
		return nil
	}
	stripeCustomerID = strings.TrimSpace(stripeCustomerID)
	workspaceID = strings.TrimSpace(workspaceID)
	if stripeCustomerID == "" || workspaceID == "" || amountCents == 0 {
		return nil
	}
	desc := "RailPush workspace credits"
	if r := strings.TrimSpace(reason); r != "" {
		desc = "RailPush credits: " + r
	}
	meta := map[string]string{"workspace_id": workspaceID, "source": "workspace_credit_ledger"}
	return s.createCustomerBalanceTransaction(stripeCustomerID, -amountCents, desc, meta)
}

// migrateWorkspaceCreditsToStripeIfNeeded performs a one-time migration of existing
// workspace credits into Stripe customer balance, so Stripe invoices can use them.
//
// After this runs successfully we rely on Ops credit grants to add new credits directly
// to Stripe and keep `credits_migrated=true` so we don't double-credit.
func (s *StripeService) migrateWorkspaceCreditsToStripeIfNeeded(bc *models.BillingCustomer, workspaceID string) error {
	if s == nil || !s.Enabled() || bc == nil {
		return nil
	}
	if bc.CreditsMigrated {
		return nil
	}
	workspaceID = strings.TrimSpace(workspaceID)
	if workspaceID == "" {
		return nil
	}

	credits, err := models.GetWorkspaceCreditBalanceCents(workspaceID)
	if err != nil {
		log.Printf("Stripe: failed to load workspace credit balance ws=%s err=%v", workspaceID, err)
		return nil
	}
	if credits <= 0 {
		bc.CreditsMigrated = true
		_ = models.UpdateBillingCustomer(bc)
		return nil
	}

	// Stripe customer balance: negative amounts are credits applied to future invoices.
	meta := map[string]string{"workspace_id": workspaceID, "source": "workspace_credit_ledger_migration"}
	if err := s.createCustomerBalanceTransaction(bc.StripeCustomerID, -credits, "RailPush workspace credits", meta); err != nil {
		log.Printf("Stripe: failed to migrate credits to Stripe customer=%s ws=%s err=%v", bc.StripeCustomerID, workspaceID, err)
		return stripeUserError(err)
	}

	bc.CreditsMigrated = true
	if err := models.UpdateBillingCustomer(bc); err != nil {
		log.Printf("Stripe: credits migrated but failed to persist billing customer flag customer=%s ws=%s err=%v", bc.ID, workspaceID, err)
		return fmt.Errorf("billing update failed. please try again")
	}
	return nil
}

func (s *StripeService) EnsureWorkspaceCreditsMigrated(bc *models.BillingCustomer, workspaceID string) error {
	return s.migrateWorkspaceCreditsToStripeIfNeeded(bc, workspaceID)
}

func (s *StripeService) CreditBalanceCents(stripeCustomerID string) (int64, error) {
	if s == nil || !s.Enabled() {
		return 0, nil
	}
	stripeCustomerID = strings.TrimSpace(stripeCustomerID)
	if stripeCustomerID == "" {
		return 0, nil
	}
	cust, err := customer.Get(stripeCustomerID, &stripe.CustomerParams{})
	if err != nil || cust == nil {
		return 0, err
	}
	if cust.Balance >= 0 {
		return 0, nil
	}
	return -cust.Balance, nil
}

// PreviewInvoice returns a preview of the next invoice for a customer/subscription.
//
// Stripe deprecated the legacy "upcoming invoice" endpoint; this uses the replacement
// create-preview endpoint.
func (s *StripeService) PreviewInvoice(stripeCustomerID, stripeSubscriptionID string) (*InvoicePreview, error) {
	if s == nil || !s.Enabled() {
		return nil, nil
	}
	stripeCustomerID = strings.TrimSpace(stripeCustomerID)
	if stripeCustomerID == "" {
		return nil, nil
	}

	form := url.Values{}
	form.Set("customer", stripeCustomerID)
	if sub := strings.TrimSpace(stripeSubscriptionID); sub != "" {
		form.Set("subscription", sub)
	}

	req, err := http.NewRequest("POST", "https://api.stripe.com/v1/invoices/create_preview", strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.SetBasicAuth(strings.TrimSpace(s.Config.Stripe.SecretKey), "")
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	client := &http.Client{Timeout: 20 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return nil, err
	}

	if resp.StatusCode >= 400 {
		var apiErr struct {
			Error struct {
				Message string `json:"message"`
			} `json:"error"`
		}
		_ = json.Unmarshal(body, &apiErr)
		if msg := sanitizeStripeMessage(apiErr.Error.Message); msg != "" {
			return nil, fmt.Errorf("%s", msg)
		}
		return nil, fmt.Errorf("failed to preview invoice")
	}

	var inv InvoicePreview
	if err := json.Unmarshal(body, &inv); err != nil {
		return nil, err
	}
	return &inv, nil
}

// AddSubscriptionItem adds a resource to the user's subscription. Creates a subscription if one doesn't exist.
// When metered billing is enabled (STRIPE_METERED_PRICE_* env vars set), creates individual metered
// subscription items per resource for per-minute billing. Otherwise falls back to flat-rate quantity billing.
func (s *StripeService) AddSubscriptionItem(bc *models.BillingCustomer, workspaceID, resourceType, resourceID, name, plan string) error {
	if bc == nil || strings.TrimSpace(bc.ID) == "" {
		return fmt.Errorf("billing customer not found")
	}

	// One-time migration: mirror existing workspace credits into Stripe customer balance so
	// they can reduce invoices (and allow deployments without a card when credits cover it).
	if err := s.migrateWorkspaceCreditsToStripeIfNeeded(bc, workspaceID); err != nil {
		return err
	}

	// Idempotency: don't double-bill the same resource if a request is retried.
	existing, err := models.GetBillingItemByResource(resourceType, resourceID)
	if err != nil {
		log.Printf("Stripe: failed to query existing billing item resource=%s/%s err=%v", resourceType, resourceID, err)
		return fmt.Errorf("billing update failed. please try again")
	}
	if existing != nil {
		// Legacy credit-billed items have no Stripe linkage. Delete and re-add so they
		// become part of the Stripe subscription moving forward.
		if strings.TrimSpace(existing.StripeSubscriptionItemID) == "" || strings.TrimSpace(existing.StripePriceID) == "" {
			if err := models.DeleteBillingItemByResource(resourceType, resourceID); err != nil {
				log.Printf("Stripe: failed to delete legacy billing item resource=%s/%s err=%v", resourceType, resourceID, err)
				return fmt.Errorf("billing update failed. please try again")
			}
		} else {
			if strings.TrimSpace(existing.Plan) != strings.TrimSpace(plan) {
				return s.UpdateSubscriptionItemPlan(resourceType, resourceID, plan)
			}
			return nil
		}
	}

	// Determine which pricing model to use: metered (per-minute) or flat-rate.
	useMetered := false
	priceID := ""
	if meteredPrice := s.MeteredPriceIDForPlan(plan); meteredPrice != "" {
		useMetered = true
		priceID = meteredPrice
	} else {
		priceID = s.PriceIDForPlan(plan)
	}
	if priceID == "" {
		return fmt.Errorf("paid plan pricing isn't configured yet. please contact support")
	}

	// Best-effort: include default payment method when present, but allow credits-only
	// subscriptions when invoice amount_due is 0 (Stripe customer balance covers it).
	defaultPMID, _ := s.getDefaultPaymentMethod(bc)

	// If no subscription exists, create one.
	if bc.StripeSubscriptionID == "" {
		itemParams := &stripe.SubscriptionItemsParams{
			Price: stripe.String(priceID),
		}
		if !useMetered {
			itemParams.Quantity = stripe.Int64(1)
		}
		subParams := &stripe.SubscriptionParams{
			Customer:          stripe.String(bc.StripeCustomerID),
			Items:             []*stripe.SubscriptionItemsParams{itemParams},
			CollectionMethod:  stripe.String(string(stripe.SubscriptionCollectionMethodChargeAutomatically)),
			PaymentBehavior:   stripe.String("error_if_incomplete"),
			OffSession:        stripe.Bool(true),
		}
		if strings.TrimSpace(defaultPMID) != "" {
			subParams.DefaultPaymentMethod = stripe.String(defaultPMID)
		}
		subParams.AddExpand("items")
		sub, err := subscription.New(subParams)
		if err != nil {
			log.Printf("Stripe: failed to create subscription customer=%s err=%v", bc.StripeCustomerID, err)
			return stripeUserError(err)
		}

		bc.StripeSubscriptionID = sub.ID
		bc.SubscriptionStatus = string(sub.Status)
		if err := models.UpdateBillingCustomer(bc); err != nil {
			log.Printf("Stripe: failed to persist billing customer subscription customer=%s sub=%s err=%v", bc.StripeCustomerID, sub.ID, err)
			return fmt.Errorf("billing update failed. please try again")
		}

		// Save the first subscription item.
		if len(sub.Items.Data) > 0 {
			bi := &models.BillingItem{
				BillingCustomerID:        bc.ID,
				StripeSubscriptionItemID: sub.Items.Data[0].ID,
				StripePriceID:            priceID,
				ResourceType:             resourceType,
				ResourceID:               resourceID,
				ResourceName:             name,
				Plan:                     plan,
			}
			if err := models.CreateBillingItem(bi); err != nil {
				log.Printf("Stripe: failed to save billing item resource=%s/%s err=%v", resourceType, resourceID, err)
				return fmt.Errorf("billing update failed. please try again")
			}
			if useMetered {
				models.SetBillingItemMetered(bi.ID, true)
			}
		}
		// Record "start" usage event for metered resources.
		if useMetered {
			_ = models.RecordUsageEvent(resourceType, resourceID, "start")
		}
		return nil
	}

	// ── Subscription exists ──

	if useMetered {
		// Metered billing: create a NEW subscription item per resource (not shared by quantity).
		// Each resource gets its own metered item so usage can be reported individually.
		siParams := &stripe.SubscriptionItemParams{
			Subscription: stripe.String(bc.StripeSubscriptionID),
			Price:        stripe.String(priceID),
			// Metered items don't set quantity; usage records determine the charge.
		}
		si, err := subscriptionitem.New(siParams)
		if err != nil {
			log.Printf("Stripe: add metered subscription item failed sub=%s price=%s resource=%s/%s err=%v", bc.StripeSubscriptionID, priceID, resourceType, resourceID, err)
			return stripeUserError(err)
		}

		bi := &models.BillingItem{
			BillingCustomerID:        bc.ID,
			StripeSubscriptionItemID: si.ID,
			StripePriceID:            priceID,
			ResourceType:             resourceType,
			ResourceID:               resourceID,
			ResourceName:             name,
			Plan:                     plan,
		}
		if err := models.CreateBillingItem(bi); err != nil {
			if _, delErr := subscriptionitem.Del(si.ID, &stripe.SubscriptionItemParams{}); delErr != nil {
				log.Printf("Stripe: CRITICAL orphaned metered subscription item si=%s resource=%s/%s — manual cleanup required: %v", si.ID, resourceType, resourceID, delErr)
			}
			log.Printf("Stripe: failed to save metered billing item resource=%s/%s err=%v", resourceType, resourceID, err)
			return fmt.Errorf("billing update failed. please try again")
		}
		models.SetBillingItemMetered(bi.ID, true)
		_ = models.RecordUsageEvent(resourceType, resourceID, "start")
		return nil
	}

	// ── Flat-rate billing (legacy path) ──

	// Stripe allows only one item per price; use quantity for multiple resources.
	existingSubItemID, err := models.FindBillingSubscriptionItemIDByCustomerAndPrice(bc.ID, priceID)
	if err != nil {
		log.Printf("Stripe: failed to lookup existing subscription item customer=%s price=%s err=%v", bc.ID, priceID, err)
		return fmt.Errorf("billing update failed. please try again")
	}

	if existingSubItemID != "" {
		// Use a transaction with FOR UPDATE to prevent concurrent quantity races.
		tx, txErr := database.DB.Begin()
		if txErr != nil {
			log.Printf("Stripe: failed to begin tx for quantity update resource=%s/%s err=%v", resourceType, resourceID, txErr)
			return fmt.Errorf("billing update failed. please try again")
		}
		defer tx.Rollback()

		// Record the resource first; then set Stripe quantity to match DB count.
		bi := &models.BillingItem{
			BillingCustomerID:        bc.ID,
			StripeSubscriptionItemID: existingSubItemID,
			StripePriceID:            priceID,
			ResourceType:             resourceType,
			ResourceID:               resourceID,
			ResourceName:             name,
			Plan:                     plan,
		}
		if err := models.CreateBillingItem(bi); err != nil {
			log.Printf("Stripe: failed to save billing item resource=%s/%s err=%v", resourceType, resourceID, err)
			return fmt.Errorf("billing update failed. please try again")
		}
		// Lock rows and count under transaction to prevent concurrent updates.
		qty, qErr := models.CountBillingItemsBySubscriptionItemIDForUpdate(tx, existingSubItemID)
		if qErr != nil || qty < 1 {
			_ = models.DeleteBillingItemByResource(resourceType, resourceID)
			if qErr != nil {
				log.Printf("Stripe: failed to count billing items for quantity reconcile sub_item=%s err=%v", existingSubItemID, qErr)
			}
			return fmt.Errorf("billing update failed. please try again")
		}
		siParams := &stripe.SubscriptionItemParams{
			Quantity: stripe.Int64(int64(qty)),
			// Avoid generating a new invoice per sync/scale operation. Using always_invoice can
			// quickly hit Stripe's daily invoice limits for a subscription when many resources
			// are added/updated in a short window (e.g. blueprint sync).
			ProrationBehavior: stripe.String("create_prorations"),
		}
		if _, err := subscriptionitem.Update(existingSubItemID, siParams); err != nil {
			log.Printf("Stripe: update quantity failed sub_item=%s qty=%d resource=%s/%s err=%v", existingSubItemID, qty, resourceType, resourceID, err)
			_ = models.DeleteBillingItemByResource(resourceType, resourceID)
			return stripeUserError(err)
		}
		if err := tx.Commit(); err != nil {
			log.Printf("Stripe: failed to commit quantity tx sub_item=%s err=%v", existingSubItemID, err)
			_ = models.DeleteBillingItemByResource(resourceType, resourceID)
			return fmt.Errorf("billing update failed. please try again")
		}
		return nil
	}

	// No existing item for this price; create one (quantity=1) then record the resource.
	siParams := &stripe.SubscriptionItemParams{
		Subscription: stripe.String(bc.StripeSubscriptionID),
		Price:        stripe.String(priceID),
		Quantity:     stripe.Int64(1),
		// See note above: do not force an invoice on every item add.
		ProrationBehavior: stripe.String("create_prorations"),
	}
	si, err := subscriptionitem.New(siParams)
	if err != nil {
		log.Printf("Stripe: add subscription item failed sub=%s price=%s resource=%s/%s err=%v", bc.StripeSubscriptionID, priceID, resourceType, resourceID, err)
		return stripeUserError(err)
	}

	bi := &models.BillingItem{
		BillingCustomerID:        bc.ID,
		StripeSubscriptionItemID: si.ID,
		StripePriceID:            priceID,
		ResourceType:             resourceType,
		ResourceID:               resourceID,
		ResourceName:             name,
		Plan:                     plan,
	}
	if err := models.CreateBillingItem(bi); err != nil {
		// Cleanup the orphaned Stripe subscription item to avoid incorrect charges.
		if _, delErr := subscriptionitem.Del(si.ID, &stripe.SubscriptionItemParams{}); delErr != nil {
			log.Printf("Stripe: CRITICAL orphaned subscription item si=%s resource=%s/%s — manual cleanup required: %v", si.ID, resourceType, resourceID, delErr)
		} else {
			log.Printf("Stripe: cleaned up orphaned subscription item si=%s after DB write failure resource=%s/%s", si.ID, resourceType, resourceID)
		}
		log.Printf("Stripe: failed to save billing item after creating subscription item resource=%s/%s err=%v", resourceType, resourceID, err)
		return fmt.Errorf("billing update failed. please try again")
	}
	return nil
}

// RemoveSubscriptionItem removes a resource's billing item. Cancels the subscription if no items remain.
// For metered items, records a "stop" usage event and reports final usage before deletion.
func (s *StripeService) RemoveSubscriptionItem(resourceType, resourceID string) error {
	bi, err := models.GetBillingItemByResource(resourceType, resourceID)
	if err != nil {
		log.Printf("Stripe: failed to query billing item resource=%s/%s err=%v", resourceType, resourceID, err)
		return fmt.Errorf("billing update failed. please try again")
	}
	if bi == nil {
		return nil // no billing item for this resource
	}

	isMetered := models.IsBillingItemMetered(resourceType, resourceID)

	// For metered items: record "stop" event and report final usage before removing.
	if isMetered {
		_ = models.RecordUsageEvent(resourceType, resourceID, "stop")
		s.reportFinalUsageForResource(bi)
	}

	subItemID := strings.TrimSpace(bi.StripeSubscriptionItemID)
	if subItemID == "" {
		_ = models.DeleteBillingItemByResource(resourceType, resourceID)
		return nil
	}

	// Remove from our DB first; if Stripe update fails, restore the DB record so the resource stays billed.
	if err := models.DeleteBillingItemByResource(resourceType, resourceID); err != nil {
		log.Printf("Stripe: failed to delete billing item resource=%s/%s err=%v", resourceType, resourceID, err)
		return fmt.Errorf("billing update failed. please try again")
	}

	if isMetered {
		// Metered items: each resource has its own subscription item — always delete it.
		if _, err := subscriptionitem.Del(subItemID, &stripe.SubscriptionItemParams{}); err != nil {
			var se *stripe.Error
			if errors.As(err, &se) && se.HTTPStatusCode == 404 {
				// Already gone.
			} else {
				log.Printf("Stripe: delete metered subscription item failed sub_item=%s resource=%s/%s err=%v", subItemID, resourceType, resourceID, err)
				_ = models.CreateBillingItem(bi)
				return stripeUserError(err)
			}
		}
	} else {
		// Flat-rate items: shared subscription item by quantity.
		remaining, countErr := models.CountBillingItemsBySubscriptionItemID(subItemID)
		if countErr != nil {
			_ = models.CreateBillingItem(bi)
			log.Printf("Stripe: failed to count remaining billing items sub_item=%s err=%v", subItemID, countErr)
			return fmt.Errorf("billing update failed. please try again")
		}

		if remaining <= 0 {
			if _, err := subscriptionitem.Del(subItemID, &stripe.SubscriptionItemParams{}); err != nil {
				var se *stripe.Error
				if errors.As(err, &se) && se.HTTPStatusCode == 404 {
					// Already gone.
				} else {
					log.Printf("Stripe: delete subscription item failed sub_item=%s resource=%s/%s err=%v", subItemID, resourceType, resourceID, err)
					_ = models.CreateBillingItem(bi)
					return stripeUserError(err)
				}
			}
		} else {
			siParams := &stripe.SubscriptionItemParams{
				Quantity:          stripe.Int64(int64(remaining)),
				ProrationBehavior: stripe.String("create_prorations"),
			}
			if _, err := subscriptionitem.Update(subItemID, siParams); err != nil {
				var se *stripe.Error
				if errors.As(err, &se) && se.HTTPStatusCode == 404 {
					log.Printf("Stripe: subscription item missing during quantity update sub_item=%s resource=%s/%s", subItemID, resourceType, resourceID)
				} else {
					log.Printf("Stripe: update quantity failed sub_item=%s qty=%d resource=%s/%s err=%v", subItemID, remaining, resourceType, resourceID, err)
					_ = models.CreateBillingItem(bi)
					return stripeUserError(err)
				}
			}
		}
	}

	// Check if subscription has remaining items; cancel if empty
	items, itemsErr := models.ListBillingItemsByCustomer(bi.BillingCustomerID)
	if itemsErr != nil {
		return nil // non-critical
	}
	if len(items) == 0 {
		var subID string
		row := database.DB.QueryRow("SELECT COALESCE(stripe_subscription_id,'') FROM billing_customers WHERE id=$1", bi.BillingCustomerID)
		if row.Scan(&subID) == nil && subID != "" {
			_, cancelErr := subscription.Cancel(subID, nil)
			if cancelErr != nil {
				log.Printf("Warning: failed to cancel empty subscription: %v", cancelErr)
			} else {
				database.DB.Exec("UPDATE billing_customers SET stripe_subscription_id='', subscription_status='canceled', updated_at=NOW() WHERE id=$1", bi.BillingCustomerID)
			}
		}
	}
	return nil
}

// reportFinalUsageForResource reports any unreported usage minutes for a metered resource.
func (s *StripeService) reportFinalUsageForResource(bi *models.BillingItem) {
	if bi == nil || strings.TrimSpace(bi.StripeSubscriptionItemID) == "" {
		return
	}
	// Determine the "since" timestamp: use last_usage_reported_at from DB.
	var since time.Time
	var lastReported sql.NullTime
	_ = database.DB.QueryRow(
		"SELECT last_usage_reported_at FROM billing_items WHERE id=$1", bi.ID,
	).Scan(&lastReported)
	if lastReported.Valid {
		since = lastReported.Time
	} else {
		since = bi.CreatedAt
	}

	now := time.Now()
	minutes, err := models.CalcActiveMinutesSince(bi.ResourceType, bi.ResourceID, since, now)
	if err != nil {
		log.Printf("Stripe: failed to calc final usage resource=%s/%s err=%v", bi.ResourceType, bi.ResourceID, err)
		return
	}
	if minutes <= 0 {
		return
	}
	if err := s.ReportUsageMinutes(bi.StripeSubscriptionItemID, minutes, now); err != nil {
		log.Printf("Stripe: failed to report final usage resource=%s/%s minutes=%d err=%v", bi.ResourceType, bi.ResourceID, minutes, err)
	} else {
		log.Printf("Stripe: reported final usage resource=%s/%s minutes=%d", bi.ResourceType, bi.ResourceID, minutes)
	}
}

// UpdateSubscriptionItemPlan changes the plan tier for an existing resource's billing item.
func (s *StripeService) UpdateSubscriptionItemPlan(resourceType, resourceID, newPlan string) error {
	bi, err := models.GetBillingItemByResource(resourceType, resourceID)
	if err != nil || bi == nil {
		return fmt.Errorf("billing item not found for resource")
	}

	newPriceID := s.PriceIDForPlan(newPlan)
	if newPriceID == "" {
		return fmt.Errorf("paid plan pricing isn't configured yet. please contact support")
	}

	oldPriceID := strings.TrimSpace(bi.StripePriceID)
	oldSubItemID := strings.TrimSpace(bi.StripeSubscriptionItemID)
	if oldPriceID == strings.TrimSpace(newPriceID) {
		bi.Plan = newPlan
		return models.UpdateBillingItem(bi)
	}

	bc, err := models.GetBillingCustomerByID(bi.BillingCustomerID)
	if err != nil || bc == nil || strings.TrimSpace(bc.StripeSubscriptionID) == "" {
		log.Printf("Stripe: billing subscription not found customer_id=%s err=%v", bi.BillingCustomerID, err)
		return fmt.Errorf("billing update failed. please try again")
	}

	// Credits migration (see AddSubscriptionItem): ensure legacy workspace credits are
	// available as Stripe customer balance before making billing changes.
	if ws, _ := models.GetWorkspaceByOwner(bc.UserID); ws != nil && strings.TrimSpace(ws.ID) != "" {
		if err := s.migrateWorkspaceCreditsToStripeIfNeeded(bc, ws.ID); err != nil {
			return err
		}
	}

	// Stripe enforces one item per price. A plan change moves this resource between price items by adjusting quantities.
	targetSubItemID, err := models.FindBillingSubscriptionItemIDByCustomerAndPrice(bc.ID, newPriceID)
	if err != nil {
		log.Printf("Stripe: failed to lookup target plan item customer=%s price=%s err=%v", bc.ID, newPriceID, err)
		return fmt.Errorf("billing update failed. please try again")
	}

	oldCount, err := models.CountBillingItemsBySubscriptionItemID(oldSubItemID)
	if err != nil {
		log.Printf("Stripe: failed to count old plan quantity sub_item=%s err=%v", oldSubItemID, err)
		return fmt.Errorf("billing update failed. please try again")
	}
	if oldCount < 1 {
		oldCount = 1
	}
	oldDesired := oldCount - 1

	newCount := 0
	if targetSubItemID != "" {
		if n, err := models.CountBillingItemsBySubscriptionItemID(targetSubItemID); err == nil {
			newCount = n
		}
	}
	newDesired := newCount + 1

	// Use context-aware proration: upgrades prorate immediately so user is charged
	// the difference; downgrades defer to end of cycle to avoid confusing credits.
	proration := "create_prorations"
	if IsDowngrade(bi.Plan, newPlan) {
		proration = "none"
	}

	subParams := &stripe.SubscriptionParams{
		PaymentBehavior:      stripe.String("error_if_incomplete"),
		ProrationBehavior:    stripe.String(proration),
		OffSession:           stripe.Bool(true),
		CollectionMethod:     stripe.String(string(stripe.SubscriptionCollectionMethodChargeAutomatically)),
		Items:                []*stripe.SubscriptionItemsParams{},
	}
	// Old item decrement/delete.
	if oldDesired <= 0 {
		subParams.Items = append(subParams.Items, &stripe.SubscriptionItemsParams{
			ID:      stripe.String(oldSubItemID),
			Deleted: stripe.Bool(true),
		})
	} else {
		subParams.Items = append(subParams.Items, &stripe.SubscriptionItemsParams{
			ID:       stripe.String(oldSubItemID),
			Quantity: stripe.Int64(int64(oldDesired)),
		})
	}
	// New item increment/add.
	if targetSubItemID != "" {
		subParams.Items = append(subParams.Items, &stripe.SubscriptionItemsParams{
			ID:       stripe.String(targetSubItemID),
			Quantity: stripe.Int64(int64(newDesired)),
		})
	} else {
		subParams.Items = append(subParams.Items, &stripe.SubscriptionItemsParams{
			Price:    stripe.String(newPriceID),
			Quantity: stripe.Int64(int64(newDesired)),
		})
	}
	subParams.AddExpand("items.data.price")
	sub, err := subscription.Update(bc.StripeSubscriptionID, subParams)
	if err != nil {
		log.Printf("Stripe: plan change failed sub=%s resource=%s/%s old_price=%s new_price=%s err=%v", bc.StripeSubscriptionID, resourceType, resourceID, oldPriceID, newPriceID, err)
		return stripeUserError(err)
	}

	// If we added a new item by price, discover its Stripe subscription item id from the updated subscription.
	finalTargetID := targetSubItemID
	if finalTargetID == "" && sub != nil && sub.Items != nil {
		for _, it := range sub.Items.Data {
			if it == nil || it.Price == nil {
				continue
			}
			if strings.TrimSpace(it.Price.ID) == strings.TrimSpace(newPriceID) {
				finalTargetID = it.ID
				break
			}
		}
	}
	if strings.TrimSpace(finalTargetID) == "" {
		return fmt.Errorf("billing update failed. please try again")
	}

	// Persist the resource's new plan mapping.
	bi.StripeSubscriptionItemID = finalTargetID
	bi.StripePriceID = newPriceID
	bi.Plan = newPlan
	if err := models.UpdateBillingItem(bi); err != nil {
		// DB write failure is unexpected; log and surface a generic message.
		log.Printf("Stripe: updated subscription but failed to persist billing item resource=%s/%s err=%v", resourceType, resourceID, err)
		return fmt.Errorf("billing update failed. please try again")
	}
	return nil
}

// HandleWebhookEvent verifies and dispatches a Stripe webhook event.
func (s *StripeService) HandleWebhookEvent(payload []byte, signature string) error {
	// Stripe may send events using an API version (release train) that doesn't match the
	// stripe-go library. Signature verification is still valid; we only rely on a small
	// subset of stable fields, so we ignore API version mismatches here.
	event, err := webhook.ConstructEventWithOptions(payload, signature, s.Config.Stripe.WebhookSecret, webhook.ConstructEventOptions{
		IgnoreAPIVersionMismatch: true,
	})
	if err != nil {
		return fmt.Errorf("%w: %w", ErrStripeWebhookSignature, err)
	}

	// Idempotency: Stripe can retry the same event multiple times (network errors, 5xx, timeouts).
	// Our handlers are mostly idempotent, but recording processed IDs prevents noisy duplicate work.
	if processed, err := s.webhookEventProcessed(event.ID); err != nil {
		return fmt.Errorf("failed to check webhook idempotency: %w", err)
	} else if processed {
		return nil
	}

	var handleErr error
	switch event.Type {
	case "checkout.session.completed":
		handleErr = s.handleCheckoutCompleted(event)
	case "customer.subscription.created":
		handleErr = s.handleSubscriptionUpdated(event)
	case "customer.subscription.updated":
		handleErr = s.handleSubscriptionUpdated(event)
	case "customer.subscription.deleted":
		handleErr = s.handleSubscriptionDeleted(event)
	case "invoice.payment_succeeded":
		handleErr = s.handlePaymentSucceeded(event)
	case "invoice.payment_failed":
		handleErr = s.handlePaymentFailed(event)
	case "charge.refunded":
		handleErr = s.handleChargeRefunded(event)
	case "charge.disputed":
		handleErr = s.handleChargeDisputed(event)
	case "customer.updated":
		handleErr = s.handleCustomerUpdated(event)
	}

	if handleErr != nil {
		return handleErr
	}

	if err := s.recordWebhookEventProcessed(event); err != nil {
		// Don't fail the webhook due to dedupe bookkeeping.
		log.Printf("Warning: failed to record stripe webhook event %s: %v", event.ID, err)
	}
	return nil
}

func (s *StripeService) webhookEventProcessed(eventID string) (bool, error) {
	eventID = strings.TrimSpace(eventID)
	if eventID == "" {
		return false, nil
	}

	var exists bool
	if err := database.DB.QueryRow("SELECT EXISTS (SELECT 1 FROM stripe_webhook_events WHERE event_id=$1)", eventID).Scan(&exists); err != nil {
		return false, err
	}
	return exists, nil
}

func (s *StripeService) recordWebhookEventProcessed(event stripe.Event) error {
	id := strings.TrimSpace(event.ID)
	if id == "" {
		return nil
	}
	_, err := database.DB.Exec(
		"INSERT INTO stripe_webhook_events (event_id, event_type, livemode, api_version, received_at, processed_at) VALUES ($1,$2,$3,$4,NOW(),NOW()) ON CONFLICT DO NOTHING",
		id,
		strings.TrimSpace(string(event.Type)),
		event.Livemode,
		strings.TrimSpace(event.APIVersion),
	)
	return err
}

func (s *StripeService) handleCheckoutCompleted(event stripe.Event) error {
	var sess stripe.CheckoutSession
	if err := json.Unmarshal(event.Data.Raw, &sess); err != nil {
		return err
	}
	if sess.Customer == nil {
		return nil
	}

	bc, err := models.GetBillingCustomerByStripeID(sess.Customer.ID)
	if err != nil || bc == nil {
		// This can happen for events related to customers created outside RailPush or
		// before a data migration. Don't fail the webhook (Stripe will retry).
		log.Printf("Webhook: billing customer not found for Stripe ID: %s", sess.Customer.ID)
		return nil
	}

	// Webhook event only includes IDs for nested objects, so we need to
	// fetch the SetupIntent with expanded PaymentMethod to get card details.
	if sess.SetupIntent != nil && sess.SetupIntent.ID != "" {
		siParams := &stripe.SetupIntentParams{}
		siParams.AddExpand("payment_method")
		si, err := setupintent.Get(sess.SetupIntent.ID, siParams)
		if err != nil {
			log.Printf("Warning: failed to fetch setup intent %s: %v", sess.SetupIntent.ID, err)
		} else if si.PaymentMethod != nil {
			pmID := si.PaymentMethod.ID

			// Use the expanded payment method details directly to avoid extra API calls.
			if si.PaymentMethod.Card != nil {
				bc.PaymentMethodLast4 = strings.TrimSpace(si.PaymentMethod.Card.Last4)
				bc.PaymentMethodBrand = strings.TrimSpace(string(si.PaymentMethod.Card.Brand))
			}

			// Ensure the payment method is attached (best-effort; Stripe may have already attached it).
			if _, err := paymentmethod.Attach(pmID, &stripe.PaymentMethodAttachParams{
				Customer: stripe.String(sess.Customer.ID),
			}); err != nil {
				log.Printf("Warning: failed to attach payment method to customer: %v", err)
			}

			// Set as default payment method on the customer
			_, err = customer.Update(sess.Customer.ID, &stripe.CustomerParams{
				InvoiceSettings: &stripe.CustomerInvoiceSettingsParams{
					DefaultPaymentMethod: stripe.String(pmID),
				},
			})
			if err != nil {
				log.Printf("Warning: failed to set default payment method: %v", err)
			}
		}
	}

	return models.UpdateBillingCustomer(bc)
}

func (s *StripeService) handleSubscriptionUpdated(event stripe.Event) error {
	var sub stripe.Subscription
	if err := json.Unmarshal(event.Data.Raw, &sub); err != nil {
		return err
	}
	if sub.Customer == nil {
		return nil
	}

	bc, err := models.GetBillingCustomerByStripeID(sub.Customer.ID)
	if err != nil || bc == nil {
		log.Printf("Webhook: billing customer not found for %s", sub.Customer.ID)
		return nil
	}

	bc.SubscriptionStatus = string(sub.Status)
	bc.StripeSubscriptionID = sub.ID

	// Update default payment method if available
	if sub.DefaultPaymentMethod != nil && sub.DefaultPaymentMethod.Card != nil {
		bc.PaymentMethodLast4 = sub.DefaultPaymentMethod.Card.Last4
		bc.PaymentMethodBrand = string(sub.DefaultPaymentMethod.Card.Brand)
	}

	return models.UpdateBillingCustomer(bc)
}

func (s *StripeService) handleSubscriptionDeleted(event stripe.Event) error {
	var sub stripe.Subscription
	if err := json.Unmarshal(event.Data.Raw, &sub); err != nil {
		return err
	}
	if sub.Customer == nil {
		return nil
	}

	bc, err := models.GetBillingCustomerByStripeID(sub.Customer.ID)
	if err != nil || bc == nil {
		return nil
	}

	bc.SubscriptionStatus = "canceled"
	bc.StripeSubscriptionID = ""
	return models.UpdateBillingCustomer(bc)
}

func (s *StripeService) handlePaymentSucceeded(event stripe.Event) error {
	var inv stripe.Invoice
	if err := json.Unmarshal(event.Data.Raw, &inv); err != nil {
		return err
	}
	if inv.Customer == nil {
		return nil
	}

	bc, err := models.GetBillingCustomerByStripeID(inv.Customer.ID)
	if err != nil || bc == nil {
		return nil
	}

	if inv.Subscription != nil && inv.Subscription.ID != "" {
		bc.StripeSubscriptionID = inv.Subscription.ID
	}
	bc.SubscriptionStatus = "active"

	// Store invoice for local reconciliation and billing history.
	billingInv := &models.BillingInvoice{
		BillingCustomerID: bc.ID,
		StripeInvoiceID:   inv.ID,
		Status:            string(inv.Status),
		AmountDueCents:    int(inv.AmountDue),
		AmountPaidCents:   int(inv.AmountPaid),
		Currency:          string(inv.Currency),
		HostedInvoiceURL:  inv.HostedInvoiceURL,
		InvoicePDFURL:     inv.InvoicePDF,
	}
	if inv.PeriodStart > 0 {
		t := time.Unix(inv.PeriodStart, 0)
		billingInv.PeriodStart = &t
	}
	if inv.PeriodEnd > 0 {
		t := time.Unix(inv.PeriodEnd, 0)
		billingInv.PeriodEnd = &t
	}
	if err := models.UpsertBillingInvoice(billingInv); err != nil {
		log.Printf("Stripe: failed to store invoice %s for customer %s: %v", inv.ID, bc.ID, err)
		// Non-fatal: don't fail the webhook for invoice storage
	}

	return models.UpdateBillingCustomer(bc)
}

func (s *StripeService) handlePaymentFailed(event stripe.Event) error {
	var inv stripe.Invoice
	if err := json.Unmarshal(event.Data.Raw, &inv); err != nil {
		return err
	}
	customerID := ""
	if inv.Customer != nil {
		customerID = inv.Customer.ID
	}
	log.Printf("Payment failed for Stripe customer %s, invoice %s", customerID, inv.ID)

	if customerID == "" {
		return nil
	}

	bc, err := models.GetBillingCustomerByStripeID(customerID)
	if err != nil || bc == nil {
		return nil
	}

	if inv.Subscription != nil && inv.Subscription.ID != "" {
		bc.StripeSubscriptionID = inv.Subscription.ID
	}
	bc.SubscriptionStatus = "past_due"

	return models.UpdateBillingCustomer(bc)
}

func (s *StripeService) handleChargeRefunded(event stripe.Event) error {
	var ch stripe.Charge
	if err := json.Unmarshal(event.Data.Raw, &ch); err != nil {
		return err
	}
	customerID := ""
	if ch.Customer != nil {
		customerID = ch.Customer.ID
	}
	log.Printf("Stripe: charge refunded customer=%s charge=%s amount_refunded=%d", customerID, ch.ID, ch.AmountRefunded)
	// Record for ops visibility; no automatic action needed.
	return nil
}

func (s *StripeService) handleChargeDisputed(event stripe.Event) error {
	var disp stripe.Dispute
	if err := json.Unmarshal(event.Data.Raw, &disp); err != nil {
		return err
	}
	customerID := ""
	if disp.Charge != nil && disp.Charge.Customer != nil {
		customerID = disp.Charge.Customer.ID
	}
	log.Printf("Stripe: charge disputed customer=%s dispute=%s amount=%d reason=%s status=%s",
		customerID, disp.ID, disp.Amount, disp.Reason, disp.Status)
	// Log the dispute for ops alerting. Future: suspend workspace if dispute is fraudulent.
	return nil
}

func (s *StripeService) handleCustomerUpdated(event stripe.Event) error {
	var cust stripe.Customer
	if err := json.Unmarshal(event.Data.Raw, &cust); err != nil {
		return err
	}
	bc, err := models.GetBillingCustomerByStripeID(cust.ID)
	if err != nil || bc == nil {
		return nil
	}
	// Sync default payment method if changed in Stripe dashboard or customer portal.
	if cust.InvoiceSettings != nil && cust.InvoiceSettings.DefaultPaymentMethod != nil {
		pm := cust.InvoiceSettings.DefaultPaymentMethod
		if pm.Card != nil {
			bc.PaymentMethodLast4 = pm.Card.Last4
			bc.PaymentMethodBrand = string(pm.Card.Brand)
			return models.UpdateBillingCustomer(bc)
		}
	}
	return nil
}

// ReadBody is a helper to read the webhook request body.
func ReadBody(body io.Reader) ([]byte, error) {
	return io.ReadAll(io.LimitReader(body, 65536))
}
