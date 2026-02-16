package services

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"regexp"
	"strings"

	"github.com/railpush/api/config"
	"github.com/railpush/api/database"
	"github.com/railpush/api/models"
	"github.com/stripe/stripe-go/v81"
	billingportalsession "github.com/stripe/stripe-go/v81/billingportal/session"
	"github.com/stripe/stripe-go/v81/checkout/session"
	"github.com/stripe/stripe-go/v81/customer"
	"github.com/stripe/stripe-go/v81/paymentmethod"
	"github.com/stripe/stripe-go/v81/setupintent"
	"github.com/stripe/stripe-go/v81/subscription"
	"github.com/stripe/stripe-go/v81/subscriptionitem"
	"github.com/stripe/stripe-go/v81/webhook"
)

type StripeService struct {
	Config *config.Config
}

var ErrNoDefaultPaymentMethod = errors.New("payment method required")
var ErrStripeWebhookSignature = errors.New("stripe webhook signature verification failed")

var stripeIDTokenRE = regexp.MustCompile(`\b(?:price|si)_[A-Za-z0-9]+\b`)
var stripeURLTokenRE = regexp.MustCompile(`https?://\S+`)

func NewStripeService(cfg *config.Config) *StripeService {
	stripe.Key = cfg.Stripe.SecretKey
	return &StripeService{Config: cfg}
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
		if msg := sanitizeStripeMessage(se.Msg); msg != "" {
			return fmt.Errorf("%s", msg)
		}
	}
	if msg := sanitizeStripeMessage(err.Error()); msg != "" {
		return fmt.Errorf("%s", msg)
	}
	return fmt.Errorf("billing update failed. please try again or contact support")
}

// EnsureCustomer creates or retrieves a Stripe customer for the given user.
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

	bc = &models.BillingCustomer{
		UserID:             userID,
		StripeCustomerID:   cust.ID,
		SubscriptionStatus: "incomplete",
	}
	if err := models.CreateBillingCustomer(bc); err != nil {
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

// AddSubscriptionItem adds a resource to the user's subscription. Creates a subscription if one doesn't exist.
// If the workspace has sufficient credits, credits are spent instead of modifying Stripe.
func (s *StripeService) AddSubscriptionItem(bc *models.BillingCustomer, workspaceID, resourceType, resourceID, name, plan string) error {
	if bc == nil || strings.TrimSpace(bc.ID) == "" {
		return fmt.Errorf("billing customer not found")
	}
	// Idempotency: don't double-bill the same resource if a request is retried.
	if existing, err := models.GetBillingItemByResource(resourceType, resourceID); err != nil {
		log.Printf("Stripe: failed to query existing billing item resource=%s/%s err=%v", resourceType, resourceID, err)
		return fmt.Errorf("billing update failed. please try again")
	} else if existing != nil {
		if strings.TrimSpace(existing.Plan) != strings.TrimSpace(plan) {
			// Credit-billed items have no Stripe linkage; update plan using credits if possible.
			if strings.TrimSpace(existing.StripeSubscriptionItemID) == "" || strings.TrimSpace(existing.StripePriceID) == "" {
				oldCost := planMonthlyCostCents(existing.Plan)
				newCost := planMonthlyCostCents(plan)
				delta := newCost - oldCost
				if delta > 0 {
					reason := fmt.Sprintf("billing: %s %s (%s) plan=%s", strings.TrimSpace(resourceType), strings.TrimSpace(name), strings.TrimSpace(resourceID), strings.TrimSpace(plan))
					spent, _, err := models.TrySpendWorkspaceCredits(workspaceID, delta, reason, bc.UserID)
					if err != nil {
						log.Printf("Stripe: failed to spend workspace credits ws=%s resource=%s/%s err=%v", workspaceID, resourceType, resourceID, err)
						return fmt.Errorf("billing update failed. please try again")
					}
					if !spent {
						return ErrNoDefaultPaymentMethod
					}
				}
				// Persist plan change locally (Stripe IDs remain blank).
				existing.Plan = plan
				if err := models.UpdateBillingItem(existing); err != nil {
					log.Printf("Stripe: failed to update credit billing item resource=%s/%s err=%v", resourceType, resourceID, err)
					return fmt.Errorf("billing update failed. please try again")
				}
				return nil
			}
			return s.UpdateSubscriptionItemPlan(resourceType, resourceID, plan)
		}
		return nil
	}

	// Credits: if the workspace has enough balance, spend credits and record the billing item locally.
	if strings.TrimSpace(workspaceID) != "" {
		cost := planMonthlyCostCents(plan)
		if cost > 0 {
			reason := fmt.Sprintf("billing: %s %s (%s) plan=%s", strings.TrimSpace(resourceType), strings.TrimSpace(name), strings.TrimSpace(resourceID), strings.TrimSpace(plan))
			if spent, _, err := models.TrySpendWorkspaceCredits(workspaceID, cost, reason, bc.UserID); err != nil {
				log.Printf("Stripe: failed to spend workspace credits ws=%s resource=%s/%s err=%v", workspaceID, resourceType, resourceID, err)
				return fmt.Errorf("billing update failed. please try again")
			} else if spent {
				bi := &models.BillingItem{
					BillingCustomerID:        bc.ID,
					StripeSubscriptionItemID: "",
					StripePriceID:            "",
					ResourceType:             resourceType,
					ResourceID:               resourceID,
					ResourceName:             name,
					Plan:                     plan,
				}
				if err := models.CreateBillingItem(bi); err != nil {
					log.Printf("Stripe: failed to save credit billing item resource=%s/%s err=%v", resourceType, resourceID, err)
					return fmt.Errorf("billing update failed. please try again")
				}
				return nil
			}
		}
	}

	priceID := s.PriceIDForPlan(plan)
	if priceID == "" {
		return fmt.Errorf("paid plan pricing isn't configured yet. please contact support")
	}

	defaultPMID, err := s.getDefaultPaymentMethod(bc)
	if err != nil {
		log.Printf("Stripe: failed to fetch default payment method customer=%s err=%v", bc.StripeCustomerID, err)
		return stripeUserError(err)
	}
	if defaultPMID == "" {
		return ErrNoDefaultPaymentMethod
	}

	// If no subscription exists, create one
	if bc.StripeSubscriptionID == "" {
		subParams := &stripe.SubscriptionParams{
			Customer: stripe.String(bc.StripeCustomerID),
			Items: []*stripe.SubscriptionItemsParams{
				{Price: stripe.String(priceID), Quantity: stripe.Int64(1)},
			},
			CollectionMethod:     stripe.String(string(stripe.SubscriptionCollectionMethodChargeAutomatically)),
			DefaultPaymentMethod: stripe.String(defaultPMID),
			PaymentBehavior:      stripe.String("error_if_incomplete"),
			OffSession:           stripe.Bool(true),
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

		// Save the first subscription item
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
		}
		return nil
	}

	// Subscription exists. Stripe allows only one item per price; use quantity for multiple resources.
	existingSubItemID, err := models.FindBillingSubscriptionItemIDByCustomerAndPrice(bc.ID, priceID)
	if err != nil {
		log.Printf("Stripe: failed to lookup existing subscription item customer=%s price=%s err=%v", bc.ID, priceID, err)
		return fmt.Errorf("billing update failed. please try again")
	}

	if existingSubItemID != "" {
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
		qty, qErr := models.CountBillingItemsBySubscriptionItemID(existingSubItemID)
		if qErr != nil || qty < 1 {
			_ = models.DeleteBillingItemByResource(resourceType, resourceID)
			if qErr != nil {
				log.Printf("Stripe: failed to count billing items for quantity reconcile sub_item=%s err=%v", existingSubItemID, qErr)
			}
			return fmt.Errorf("billing update failed. please try again")
		}
		siParams := &stripe.SubscriptionItemParams{
			Quantity:          stripe.Int64(int64(qty)),
			PaymentBehavior:   stripe.String("error_if_incomplete"),
			ProrationBehavior: stripe.String("always_invoice"),
		}
		if _, err := subscriptionitem.Update(existingSubItemID, siParams); err != nil {
			log.Printf("Stripe: update quantity failed sub_item=%s qty=%d resource=%s/%s err=%v", existingSubItemID, qty, resourceType, resourceID, err)
			_ = models.DeleteBillingItemByResource(resourceType, resourceID)
			return stripeUserError(err)
		}
		return nil
	}

	// No existing item for this price; create one (quantity=1) then record the resource.
	siParams := &stripe.SubscriptionItemParams{
		Subscription:      stripe.String(bc.StripeSubscriptionID),
		Price:             stripe.String(priceID),
		Quantity:          stripe.Int64(1),
		PaymentBehavior:   stripe.String("error_if_incomplete"),
		ProrationBehavior: stripe.String("always_invoice"),
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
		// Best-effort cleanup; leaving an orphan subscription item would charge incorrectly.
		_, _ = subscriptionitem.Del(si.ID, &stripe.SubscriptionItemParams{})
		log.Printf("Stripe: failed to save billing item after creating subscription item resource=%s/%s err=%v", resourceType, resourceID, err)
		return fmt.Errorf("billing update failed. please try again")
	}
	return nil
}

// RemoveSubscriptionItem removes a resource's billing item. Cancels the subscription if no items remain.
func (s *StripeService) RemoveSubscriptionItem(resourceType, resourceID string) error {
	bi, err := models.GetBillingItemByResource(resourceType, resourceID)
	if err != nil {
		log.Printf("Stripe: failed to query billing item resource=%s/%s err=%v", resourceType, resourceID, err)
		return fmt.Errorf("billing update failed. please try again")
	}
	if bi == nil {
		return nil // no billing item for this resource
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

	remaining, countErr := models.CountBillingItemsBySubscriptionItemID(subItemID)
	if countErr != nil {
		// Best-effort: if we can't count, do not change Stripe; restore DB row to avoid underbilling.
		_ = models.CreateBillingItem(bi)
		log.Printf("Stripe: failed to count remaining billing items sub_item=%s err=%v", subItemID, countErr)
		return fmt.Errorf("billing update failed. please try again")
	}

	if remaining <= 0 {
		// Last resource on this plan: delete the Stripe subscription item.
		if _, err := subscriptionitem.Del(subItemID, &stripe.SubscriptionItemParams{}); err != nil {
			var se *stripe.Error
			if errors.As(err, &se) && se.HTTPStatusCode == 404 {
				// Already gone; treat as success.
			} else {
				log.Printf("Stripe: delete subscription item failed sub_item=%s resource=%s/%s err=%v", subItemID, resourceType, resourceID, err)
				_ = models.CreateBillingItem(bi)
				return stripeUserError(err)
			}
		}
	} else {
		// Still have resources on this plan: set quantity to remaining.
		siParams := &stripe.SubscriptionItemParams{
			Quantity:          stripe.Int64(int64(remaining)),
			PaymentBehavior:   stripe.String("error_if_incomplete"),
			ProrationBehavior: stripe.String("always_invoice"),
		}
		if _, err := subscriptionitem.Update(subItemID, siParams); err != nil {
			var se *stripe.Error
			if errors.As(err, &se) && se.HTTPStatusCode == 404 {
				// Missing item means no billing; restore DB so it can be re-added later.
				log.Printf("Stripe: subscription item missing during quantity update sub_item=%s resource=%s/%s", subItemID, resourceType, resourceID)
				_ = models.CreateBillingItem(bi)
				return fmt.Errorf("billing update failed. please try again")
			}
			log.Printf("Stripe: update quantity failed sub_item=%s qty=%d resource=%s/%s err=%v", subItemID, remaining, resourceType, resourceID, err)
			_ = models.CreateBillingItem(bi)
			return stripeUserError(err)
		}
	}

	// Check if subscription has remaining items; cancel if empty
	items, itemsErr := models.ListBillingItemsByCustomer(bi.BillingCustomerID)
	if itemsErr != nil {
		return nil // non-critical
	}
	if len(items) == 0 {
		// Look up the billing customer to get the subscription ID
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

	subParams := &stripe.SubscriptionParams{
		PaymentBehavior:      stripe.String("error_if_incomplete"),
		ProrationBehavior:    stripe.String("always_invoice"),
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

// ReadBody is a helper to read the webhook request body.
func ReadBody(body io.Reader) ([]byte, error) {
	return io.ReadAll(io.LimitReader(body, 65536))
}
