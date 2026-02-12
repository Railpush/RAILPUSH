package handlers

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/gorilla/mux"
	"github.com/railpush/api/config"
	"github.com/railpush/api/middleware"
	"github.com/railpush/api/models"
	"github.com/railpush/api/services/registrar"
	"github.com/railpush/api/utils"
)

type RegisteredDomainHandler struct {
	Config    *config.Config
	Registrar *registrar.ProviderRouter
}

func NewRegisteredDomainHandler(cfg *config.Config, reg *registrar.ProviderRouter) *RegisteredDomainHandler {
	return &RegisteredDomainHandler{Config: cfg, Registrar: reg}
}

func (h *RegisteredDomainHandler) SearchDomains(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Query string   `json:"query"`
		TLDs  []string `json:"tlds"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Query == "" {
		utils.RespondError(w, http.StatusBadRequest, "query is required")
		return
	}
	adapter := h.Registrar.Default()
	results, err := adapter.SearchAvailability(req.Query, req.TLDs)
	if err != nil {
		utils.RespondError(w, http.StatusInternalServerError, "search failed: "+err.Error())
		return
	}
	utils.RespondJSON(w, http.StatusOK, results)
}

func (h *RegisteredDomainHandler) RegisterDomain(w http.ResponseWriter, r *http.Request) {
	userID := middleware.GetUserID(r)
	var req struct {
		Domain string `json:"domain"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Domain == "" {
		utils.RespondError(w, http.StatusBadRequest, "domain is required")
		return
	}
	domain := strings.ToLower(strings.TrimSpace(req.Domain))

	adapter := h.Registrar.ForDomain(domain)

	// Check availability
	avail, err := adapter.CheckAvailability(domain)
	if err != nil {
		utils.RespondError(w, http.StatusInternalServerError, "availability check failed: "+err.Error())
		return
	}
	if !avail.Available {
		utils.RespondError(w, http.StatusConflict, "domain is not available")
		return
	}

	// Register with provider
	result, err := adapter.Register(domain, 1)
	if err != nil {
		utils.RespondError(w, http.StatusInternalServerError, "registration failed: "+err.Error())
		return
	}

	// Extract TLD
	parts := strings.SplitN(domain, ".", 2)
	tld := ""
	if len(parts) == 2 {
		tld = parts[1]
	}

	// Save to DB
	d := &models.RegisteredDomain{
		UserID:           userID,
		DomainName:       domain,
		TLD:              tld,
		Provider:         "mock",
		ProviderDomainID: result.ProviderDomainID,
		Status:           "active",
		ExpiresAt:        &result.ExpiresAt,
		AutoRenew:        true,
		WhoisPrivacy:     true,
		CostCents:        avail.PriceCents,
		SellCents:        avail.PriceCents,
	}
	if err := models.CreateRegisteredDomain(d); err != nil {
		utils.RespondError(w, http.StatusInternalServerError, "failed to save domain: "+err.Error())
		return
	}

	// Record transaction
	models.CreateDomainTransaction(d.ID, userID, "registration", avail.PriceCents, "completed")

	// Create default DNS records
	defaultRecords := []models.DnsRecord{
		{DomainID: d.ID, RecordType: "A", Name: "@", Value: "142.132.255.45", TTL: 3600, Managed: true},
		{DomainID: d.ID, RecordType: "CNAME", Name: "www", Value: domain + ".", TTL: 3600, Managed: true},
		{DomainID: d.ID, RecordType: "MX", Name: "@", Value: "mail." + domain + ".", TTL: 3600, Priority: 10, Managed: false},
		{DomainID: d.ID, RecordType: "TXT", Name: "@", Value: "v=spf1 ~all", TTL: 3600, Managed: false},
	}
	for i := range defaultRecords {
		models.CreateDnsRecord(&defaultRecords[i])
	}

	utils.RespondJSON(w, http.StatusCreated, d)
}

func (h *RegisteredDomainHandler) ListDomains(w http.ResponseWriter, r *http.Request) {
	userID := middleware.GetUserID(r)
	domains, err := models.ListRegisteredDomainsByUser(userID)
	if err != nil {
		utils.RespondError(w, http.StatusInternalServerError, "failed to list domains")
		return
	}
	if domains == nil {
		domains = []models.RegisteredDomain{}
	}
	utils.RespondJSON(w, http.StatusOK, domains)
}

func (h *RegisteredDomainHandler) GetDomain(w http.ResponseWriter, r *http.Request) {
	userID := middleware.GetUserID(r)
	id := mux.Vars(r)["id"]
	d, err := models.GetRegisteredDomain(id)
	if err != nil {
		utils.RespondError(w, http.StatusInternalServerError, "failed to get domain")
		return
	}
	if d == nil {
		utils.RespondError(w, http.StatusNotFound, "domain not found")
		return
	}
	if d.UserID != userID {
		utils.RespondError(w, http.StatusForbidden, "access denied")
		return
	}
	utils.RespondJSON(w, http.StatusOK, d)
}

func (h *RegisteredDomainHandler) UpdateDomain(w http.ResponseWriter, r *http.Request) {
	userID := middleware.GetUserID(r)
	id := mux.Vars(r)["id"]
	d, err := models.GetRegisteredDomain(id)
	if err != nil || d == nil {
		utils.RespondError(w, http.StatusNotFound, "domain not found")
		return
	}
	if d.UserID != userID {
		utils.RespondError(w, http.StatusForbidden, "access denied")
		return
	}

	var req struct {
		AutoRenew    *bool `json:"auto_renew"`
		WhoisPrivacy *bool `json:"whois_privacy"`
		Locked       *bool `json:"locked"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		utils.RespondError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.AutoRenew != nil {
		d.AutoRenew = *req.AutoRenew
	}
	if req.WhoisPrivacy != nil {
		d.WhoisPrivacy = *req.WhoisPrivacy
	}
	if req.Locked != nil {
		d.Locked = *req.Locked
	}
	if err := models.UpdateRegisteredDomain(d); err != nil {
		utils.RespondError(w, http.StatusInternalServerError, "failed to update domain")
		return
	}
	utils.RespondJSON(w, http.StatusOK, d)
}

func (h *RegisteredDomainHandler) DeleteDomain(w http.ResponseWriter, r *http.Request) {
	userID := middleware.GetUserID(r)
	id := mux.Vars(r)["id"]
	d, err := models.GetRegisteredDomain(id)
	if err != nil || d == nil {
		utils.RespondError(w, http.StatusNotFound, "domain not found")
		return
	}
	if d.UserID != userID {
		utils.RespondError(w, http.StatusForbidden, "access denied")
		return
	}
	if err := models.UpdateRegisteredDomainStatus(id, "cancelled"); err != nil {
		utils.RespondError(w, http.StatusInternalServerError, "failed to cancel domain")
		return
	}
	utils.RespondJSON(w, http.StatusOK, map[string]string{"status": "cancelled"})
}

func (h *RegisteredDomainHandler) RenewDomain(w http.ResponseWriter, r *http.Request) {
	userID := middleware.GetUserID(r)
	id := mux.Vars(r)["id"]
	d, err := models.GetRegisteredDomain(id)
	if err != nil || d == nil {
		utils.RespondError(w, http.StatusNotFound, "domain not found")
		return
	}
	if d.UserID != userID {
		utils.RespondError(w, http.StatusForbidden, "access denied")
		return
	}

	adapter := h.Registrar.ForDomain(d.DomainName)
	result, err := adapter.Renew(d.ProviderDomainID, 1)
	if err != nil {
		utils.RespondError(w, http.StatusInternalServerError, "renewal failed: "+err.Error())
		return
	}

	models.UpdateRegisteredDomainExpiry(id, result.ExpiresAt)
	models.CreateDomainTransaction(id, userID, "renewal", d.CostCents, "completed")

	d.ExpiresAt = &result.ExpiresAt
	utils.RespondJSON(w, http.StatusOK, d)
}

// DNS Record handlers

var validRecordTypes = map[string]bool{
	"A": true, "AAAA": true, "CNAME": true, "MX": true, "TXT": true, "NS": true, "SRV": true, "CAA": true,
}

func (h *RegisteredDomainHandler) ListDnsRecords(w http.ResponseWriter, r *http.Request) {
	userID := middleware.GetUserID(r)
	domainID := mux.Vars(r)["id"]
	d, err := models.GetRegisteredDomain(domainID)
	if err != nil || d == nil {
		utils.RespondError(w, http.StatusNotFound, "domain not found")
		return
	}
	if d.UserID != userID {
		utils.RespondError(w, http.StatusForbidden, "access denied")
		return
	}
	records, err := models.ListDnsRecordsByDomain(domainID)
	if err != nil {
		utils.RespondError(w, http.StatusInternalServerError, "failed to list records")
		return
	}
	if records == nil {
		records = []models.DnsRecord{}
	}
	utils.RespondJSON(w, http.StatusOK, records)
}

func (h *RegisteredDomainHandler) CreateDnsRecord(w http.ResponseWriter, r *http.Request) {
	userID := middleware.GetUserID(r)
	domainID := mux.Vars(r)["id"]
	d, err := models.GetRegisteredDomain(domainID)
	if err != nil || d == nil {
		utils.RespondError(w, http.StatusNotFound, "domain not found")
		return
	}
	if d.UserID != userID {
		utils.RespondError(w, http.StatusForbidden, "access denied")
		return
	}

	var req struct {
		RecordType string `json:"record_type"`
		Name       string `json:"name"`
		Value      string `json:"value"`
		TTL        int    `json:"ttl"`
		Priority   int    `json:"priority"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		utils.RespondError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if !validRecordTypes[strings.ToUpper(req.RecordType)] {
		utils.RespondError(w, http.StatusBadRequest, "invalid record type")
		return
	}
	if req.Name == "" || req.Value == "" {
		utils.RespondError(w, http.StatusBadRequest, "name and value are required")
		return
	}
	if req.TTL <= 0 {
		req.TTL = 3600
	}

	rec := &models.DnsRecord{
		DomainID:   domainID,
		RecordType: strings.ToUpper(req.RecordType),
		Name:       req.Name,
		Value:      req.Value,
		TTL:        req.TTL,
		Priority:   req.Priority,
		Managed:    false,
	}
	if err := models.CreateDnsRecord(rec); err != nil {
		utils.RespondError(w, http.StatusInternalServerError, "failed to create record: "+err.Error())
		return
	}
	utils.RespondJSON(w, http.StatusCreated, rec)
}

func (h *RegisteredDomainHandler) UpdateDnsRecord(w http.ResponseWriter, r *http.Request) {
	userID := middleware.GetUserID(r)
	domainID := mux.Vars(r)["id"]
	recordID := mux.Vars(r)["recordId"]

	d, err := models.GetRegisteredDomain(domainID)
	if err != nil || d == nil {
		utils.RespondError(w, http.StatusNotFound, "domain not found")
		return
	}
	if d.UserID != userID {
		utils.RespondError(w, http.StatusForbidden, "access denied")
		return
	}

	rec, err := models.GetDnsRecord(recordID)
	if err != nil || rec == nil {
		utils.RespondError(w, http.StatusNotFound, "record not found")
		return
	}
	if rec.DomainID != domainID {
		utils.RespondError(w, http.StatusNotFound, "record not found")
		return
	}
	if rec.Managed {
		utils.RespondError(w, http.StatusForbidden, "managed records cannot be edited")
		return
	}

	var req struct {
		RecordType string `json:"record_type"`
		Name       string `json:"name"`
		Value      string `json:"value"`
		TTL        int    `json:"ttl"`
		Priority   int    `json:"priority"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		utils.RespondError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.RecordType != "" {
		if !validRecordTypes[strings.ToUpper(req.RecordType)] {
			utils.RespondError(w, http.StatusBadRequest, "invalid record type")
			return
		}
		rec.RecordType = strings.ToUpper(req.RecordType)
	}
	if req.Name != "" {
		rec.Name = req.Name
	}
	if req.Value != "" {
		rec.Value = req.Value
	}
	if req.TTL > 0 {
		rec.TTL = req.TTL
	}
	rec.Priority = req.Priority

	if err := models.UpdateDnsRecord(rec); err != nil {
		utils.RespondError(w, http.StatusInternalServerError, "failed to update record")
		return
	}
	utils.RespondJSON(w, http.StatusOK, rec)
}

func (h *RegisteredDomainHandler) DeleteDnsRecord(w http.ResponseWriter, r *http.Request) {
	userID := middleware.GetUserID(r)
	domainID := mux.Vars(r)["id"]
	recordID := mux.Vars(r)["recordId"]

	d, err := models.GetRegisteredDomain(domainID)
	if err != nil || d == nil {
		utils.RespondError(w, http.StatusNotFound, "domain not found")
		return
	}
	if d.UserID != userID {
		utils.RespondError(w, http.StatusForbidden, "access denied")
		return
	}

	rec, err := models.GetDnsRecord(recordID)
	if err != nil || rec == nil {
		utils.RespondError(w, http.StatusNotFound, "record not found")
		return
	}
	if rec.DomainID != domainID {
		utils.RespondError(w, http.StatusNotFound, "record not found")
		return
	}
	if rec.Managed {
		utils.RespondError(w, http.StatusForbidden, "managed records cannot be deleted")
		return
	}

	if err := models.DeleteDnsRecord(recordID); err != nil {
		utils.RespondError(w, http.StatusInternalServerError, "failed to delete record")
		return
	}
	utils.RespondJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}
