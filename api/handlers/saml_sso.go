package handlers

import (
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/gorilla/mux"
	"github.com/railpush/api/config"
	"github.com/railpush/api/middleware"
	"github.com/railpush/api/models"
	"github.com/railpush/api/services"
	"github.com/railpush/api/utils"
	"github.com/russellhaering/gosaml2"
	dsig "github.com/russellhaering/goxmldsig"
)

type SamlSSOHandler struct {
	Config *config.Config
}

func NewSamlSSOHandler(cfg *config.Config) *SamlSSOHandler {
	return &SamlSSOHandler{Config: cfg}
}

func (h *SamlSSOHandler) GetConfig(w http.ResponseWriter, r *http.Request) {
	userID := middleware.GetUserID(r)
	workspaceID := mux.Vars(r)["id"]
	if err := services.EnsureWorkspaceAccess(userID, workspaceID, models.RoleAdmin); err != nil {
		utils.RespondError(w, http.StatusForbidden, "forbidden")
		return
	}
	cfg, err := models.GetSamlSSOConfig(workspaceID)
	if err != nil {
		utils.RespondError(w, http.StatusInternalServerError, "failed to load SAML config")
		return
	}
	if cfg == nil {
		defaultACS := fmt.Sprintf("https://%s/api/v1/workspaces/%s/sso/saml/acs", h.Config.Deploy.Domain, workspaceID)
		cfg = &models.SamlSSOConfig{
			WorkspaceID: workspaceID,
			Enabled:     false,
			EntityID:    "railpush-" + workspaceID,
			ACSURL:      defaultACS,
		}
	}
	utils.RespondJSON(w, http.StatusOK, cfg)
}

func (h *SamlSSOHandler) UpsertConfig(w http.ResponseWriter, r *http.Request) {
	userID := middleware.GetUserID(r)
	workspaceID := mux.Vars(r)["id"]
	if err := services.EnsureWorkspaceAccess(userID, workspaceID, models.RoleAdmin); err != nil {
		utils.RespondError(w, http.StatusForbidden, "forbidden")
		return
	}
	var req models.SamlSSOConfig
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		utils.RespondError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	req.WorkspaceID = workspaceID
	req.EntityID = strings.TrimSpace(req.EntityID)
	req.ACSURL = strings.TrimSpace(req.ACSURL)
	if req.EntityID == "" || req.ACSURL == "" {
		utils.RespondError(w, http.StatusBadRequest, "entity_id and acs_url are required")
		return
	}
	for i, d := range req.AllowedDomains {
		req.AllowedDomains[i] = strings.ToLower(strings.TrimSpace(d))
	}
	if err := models.UpsertSamlSSOConfig(&req); err != nil {
		utils.RespondError(w, http.StatusInternalServerError, "failed to save SAML config")
		return
	}
	services.Audit(workspaceID, userID, "saml.config_updated", "workspace", workspaceID, map[string]interface{}{
		"enabled":         req.Enabled,
		"entity_id":       req.EntityID,
		"metadata_url":    req.MetadataURL,
		"idp_sso_url":     req.IDPSSOURL,
		"allowed_domains": req.AllowedDomains,
	})
	utils.RespondJSON(w, http.StatusOK, req)
}

func (h *SamlSSOHandler) Metadata(w http.ResponseWriter, r *http.Request) {
	workspaceID := mux.Vars(r)["id"]
	cfg, err := models.GetSamlSSOConfig(workspaceID)
	if err != nil || cfg == nil {
		utils.RespondError(w, http.StatusNotFound, "saml config not found")
		return
	}
	w.Header().Set("Content-Type", "application/samlmetadata+xml")
	metadata := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<EntityDescriptor entityID="%s" xmlns="urn:oasis:names:tc:SAML:2.0:metadata">
  <SPSSODescriptor protocolSupportEnumeration="urn:oasis:names:tc:SAML:2.0:protocol">
    <AssertionConsumerService Binding="urn:oasis:names:tc:SAML:2.0:bindings:HTTP-POST" Location="%s" index="0" isDefault="true"/>
  </SPSSODescriptor>
</EntityDescriptor>`, cfg.EntityID, cfg.ACSURL)
	_, _ = w.Write([]byte(metadata))
}

func parseFirstCertificate(pemBundle string) (*x509.Certificate, error) {
	rest := []byte(strings.TrimSpace(pemBundle))
	for len(rest) > 0 {
		block, next := pem.Decode(rest)
		if block == nil {
			break
		}
		rest = next
		if block.Type != "CERTIFICATE" {
			continue
		}
		cert, err := x509.ParseCertificate(block.Bytes)
		if err != nil {
			return nil, err
		}
		return cert, nil
	}
	return nil, fmt.Errorf("no certificate found")
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

func extractSAMLIdentity(assertionInfo *saml2.AssertionInfo) (email, name string) {
	if assertionInfo == nil {
		return "", ""
	}
	email = firstNonEmpty(
		assertionInfo.Values.Get("email"),
		assertionInfo.Values.Get("mail"),
		assertionInfo.Values.Get("Email"),
		assertionInfo.Values.Get("emailAddress"),
		assertionInfo.Values.Get("http://schemas.xmlsoap.org/ws/2005/05/identity/claims/emailaddress"),
	)
	if email == "" && strings.Contains(assertionInfo.NameID, "@") {
		email = assertionInfo.NameID
	}
	name = firstNonEmpty(
		assertionInfo.Values.Get("name"),
		assertionInfo.Values.Get("displayName"),
		assertionInfo.Values.Get("given_name"),
		assertionInfo.Values.Get("http://schemas.xmlsoap.org/ws/2005/05/identity/claims/name"),
		assertionInfo.NameID,
	)
	return strings.ToLower(strings.TrimSpace(email)), strings.TrimSpace(name)
}

func (h *SamlSSOHandler) ACS(w http.ResponseWriter, r *http.Request) {
	workspaceID := mux.Vars(r)["id"]
	cfg, err := models.GetSamlSSOConfig(workspaceID)
	if err != nil || cfg == nil || !cfg.Enabled {
		utils.RespondError(w, http.StatusBadRequest, "saml sso is not enabled")
		return
	}

	if strings.TrimSpace(cfg.IDPCertPEM) == "" {
		utils.RespondError(w, http.StatusBadRequest, "idp certificate is required")
		return
	}
	idpCert, err := parseFirstCertificate(cfg.IDPCertPEM)
	if err != nil {
		utils.RespondError(w, http.StatusBadRequest, "invalid idp certificate")
		return
	}

	if err := r.ParseForm(); err != nil {
		utils.RespondError(w, http.StatusBadRequest, "invalid SAML response body")
		return
	}
	encodedResp := strings.TrimSpace(r.FormValue("SAMLResponse"))
	if encodedResp == "" {
		utils.RespondError(w, http.StatusBadRequest, "SAMLResponse is required")
		return
	}

	sp := &saml2.SAMLServiceProvider{
		IdentityProviderSSOURL:      strings.TrimSpace(cfg.IDPSSOURL),
		AssertionConsumerServiceURL: strings.TrimSpace(cfg.ACSURL),
		ServiceProviderIssuer:       strings.TrimSpace(cfg.EntityID),
		AudienceURI:                 strings.TrimSpace(cfg.EntityID),
		IDPCertificateStore: &dsig.MemoryX509CertificateStore{
			Roots: []*x509.Certificate{idpCert},
		},
		MaximumDecompressedBodySize: 10 * 1024 * 1024,
	}
	assertionInfo, err := sp.RetrieveAssertionInfo(encodedResp)
	if err != nil || assertionInfo == nil {
		utils.RespondError(w, http.StatusUnauthorized, "invalid SAML assertion")
		return
	}
	if len(assertionInfo.Assertions) == 0 {
		utils.RespondError(w, http.StatusUnauthorized, "missing SAML assertion")
		return
	}
	email, name := extractSAMLIdentity(assertionInfo)
	if email == "" {
		utils.RespondError(w, http.StatusUnauthorized, "email claim is required from SAML assertion")
		return
	}

	if len(cfg.AllowedDomains) > 0 {
		idx := strings.LastIndex(email, "@")
		domain := ""
		if idx >= 0 && idx+1 < len(email) {
			domain = email[idx+1:]
		}
		ok := false
		for _, allowed := range cfg.AllowedDomains {
			if domain == allowed {
				ok = true
				break
			}
		}
		if !ok {
			utils.RespondError(w, http.StatusForbidden, "email domain is not allowed for this workspace")
			return
		}
	}

	u, err := models.GetUserByEmail(email)
	if err != nil {
		utils.RespondError(w, http.StatusInternalServerError, "database error")
		return
	}
	if u == nil {
		u = &models.User{
			Username: name,
			Email:    email,
		}
		if u.Username == "" {
			u.Username = email
		}
		if err := models.CreateUser(u); err != nil {
			utils.RespondError(w, http.StatusInternalServerError, "failed to create user")
			return
		}
	}
	_ = models.AddWorkspaceMember(workspaceID, u.ID, models.RoleViewer)

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.RegisteredClaims{
		Subject:   u.ID,
		ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Duration(h.Config.JWT.Expiration) * time.Hour)),
		IssuedAt:  jwt.NewNumericDate(time.Now()),
	})
	tokenStr, err := token.SignedString([]byte(h.Config.JWT.Secret))
	if err != nil {
		utils.RespondError(w, http.StatusInternalServerError, "failed to generate token")
		return
	}
	setSessionCookie(w, r, tokenStr, time.Duration(h.Config.JWT.Expiration)*time.Hour)
	services.Audit(workspaceID, u.ID, "saml.login", "workspace", workspaceID, map[string]interface{}{
		"email": email,
	})
	utils.RespondJSON(w, http.StatusOK, map[string]interface{}{
		"user": u,
	})
}
