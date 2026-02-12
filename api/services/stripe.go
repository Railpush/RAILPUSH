package services

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"

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

func NewStripeService(cfg *config.Config) *StripeService {
	stripe.Key = cfg.Stripe.SecretKey
	return &StripeService{Config: cfg}
}

func (s *StripeService) Enabled() bool {
	return s.Config.Stripe.SecretKey != ""
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

// EnsureCustomer creates or retrieves a Stripe customer for the given user.
func (s *StripeService) EnsureCustomer(userID, email string) (*models.BillingCustomer, error) {
	bc, err := models.GetBillingCustomerByUserID(userID)
	if err != nil {
		return nil, fmt.Errorf("failed to query billing customer: %w", err)
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
		return nil, fmt.Errorf("failed to create Stripe customer: %w", err)
	}

	bc = &models.BillingCustomer{
		UserID:             userID,
		StripeCustomerID:   cust.ID,
		SubscriptionStatus: "incomplete",
	}
	if err := models.CreateBillingCustomer(bc); err != nil {
		return nil, fmt.Errorf("failed to save billing customer: %w", err)
	}
	return bc, nil
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
		return "", fmt.Errorf("failed to create checkout session: %w", err)
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
		return "", fmt.Errorf("failed to create portal session: %w", err)
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
func (s *StripeService) AddSubscriptionItem(bc *models.BillingCustomer, resourceType, resourceID, name, plan string) error {
	priceID := s.PriceIDForPlan(plan)
	if priceID == "" {
		return fmt.Errorf("no Stripe price configured for plan: %s", plan)
	}

	defaultPMID, err := s.getDefaultPaymentMethod(bc)
	if err != nil {
		return err
	}
	if defaultPMID == "" {
		return ErrNoDefaultPaymentMethod
	}

	// If no subscription exists, create one
	if bc.StripeSubscriptionID == "" {
		subParams := &stripe.SubscriptionParams{
			Customer: stripe.String(bc.StripeCustomerID),
			Items: []*stripe.SubscriptionItemsParams{
				{Price: stripe.String(priceID)},
			},
			CollectionMethod:     stripe.String(string(stripe.SubscriptionCollectionMethodChargeAutomatically)),
			DefaultPaymentMethod: stripe.String(defaultPMID),
			PaymentBehavior:      stripe.String("error_if_incomplete"),
			OffSession:           stripe.Bool(true),
		}
		subParams.AddExpand("items")
		sub, err := subscription.New(subParams)
		if err != nil {
			return fmt.Errorf("failed to create subscription: %w", err)
		}

		bc.StripeSubscriptionID = sub.ID
		bc.SubscriptionStatus = string(sub.Status)
		if err := models.UpdateBillingCustomer(bc); err != nil {
			return fmt.Errorf("failed to update billing customer subscription: %w", err)
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
				return fmt.Errorf("failed to save billing item: %w", err)
			}
		}
		return nil
	}

	// Subscription exists, add an item
	siParams := &stripe.SubscriptionItemParams{
		Subscription:      stripe.String(bc.StripeSubscriptionID),
		Price:             stripe.String(priceID),
		OffSession:        stripe.Bool(true),
		PaymentBehavior:   stripe.String("error_if_incomplete"),
		ProrationBehavior: stripe.String("always_invoice"),
	}
	si, err := subscriptionitem.New(siParams)
	if err != nil {
		return fmt.Errorf("failed to add subscription item: %w", err)
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
		return fmt.Errorf("failed to save billing item: %w", err)
	}
	return nil
}

// RemoveSubscriptionItem removes a resource's billing item. Cancels the subscription if no items remain.
func (s *StripeService) RemoveSubscriptionItem(resourceType, resourceID string) error {
	bi, err := models.GetBillingItemByResource(resourceType, resourceID)
	if err != nil {
		return fmt.Errorf("failed to query billing item: %w", err)
	}
	if bi == nil {
		return nil // no billing item for this resource
	}

	// Delete the subscription item from Stripe
	_, err = subscriptionitem.Del(bi.StripeSubscriptionItemID, &stripe.SubscriptionItemParams{})
	if err != nil {
		log.Printf("Warning: failed to delete Stripe subscription item %s: %v", bi.StripeSubscriptionItemID, err)
	}

	// Delete from our DB
	if err := models.DeleteBillingItemByResource(resourceType, resourceID); err != nil {
		return fmt.Errorf("failed to delete billing item: %w", err)
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
		return fmt.Errorf("no Stripe price configured for plan: %s", newPlan)
	}

	siParams := &stripe.SubscriptionItemParams{
		Price: stripe.String(newPriceID),
	}
	_, err = subscriptionitem.Update(bi.StripeSubscriptionItemID, siParams)
	if err != nil {
		return fmt.Errorf("failed to update subscription item: %w", err)
	}

	bi.StripePriceID = newPriceID
	bi.Plan = newPlan
	return models.UpdateBillingItem(bi)
}

// HandleWebhookEvent verifies and dispatches a Stripe webhook event.
func (s *StripeService) HandleWebhookEvent(payload []byte, signature string) error {
	event, err := webhook.ConstructEvent(payload, signature, s.Config.Stripe.WebhookSecret)
	if err != nil {
		return fmt.Errorf("webhook signature verification failed: %w", err)
	}

	switch event.Type {
	case "checkout.session.completed":
		return s.handleCheckoutCompleted(event)
	case "customer.subscription.updated":
		return s.handleSubscriptionUpdated(event)
	case "customer.subscription.deleted":
		return s.handleSubscriptionDeleted(event)
	case "invoice.payment_succeeded":
		return s.handlePaymentSucceeded(event)
	case "invoice.payment_failed":
		return s.handlePaymentFailed(event)
	}
	return nil
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
		return fmt.Errorf("billing customer not found for Stripe ID: %s", sess.Customer.ID)
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

			// Fetch full payment method details
			pm, err := paymentmethod.Get(pmID, nil)
			if err != nil {
				log.Printf("Warning: failed to fetch payment method %s: %v", pmID, err)
			} else if pm.Card != nil {
				bc.PaymentMethodLast4 = pm.Card.Last4
				bc.PaymentMethodBrand = string(pm.Card.Brand)
			}

			// Attach the payment method as the customer's default so subscriptions can charge it
			_, err = paymentmethod.Attach(pmID, &stripe.PaymentMethodAttachParams{
				Customer: stripe.String(sess.Customer.ID),
			})
			if err != nil {
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
