package handlers

import (
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/gorilla/mux"
	"github.com/railpush/api/config"
	"github.com/railpush/api/middleware"
	"github.com/railpush/api/models"
	"github.com/railpush/api/services"
	"github.com/railpush/api/utils"
)

type AuthHandler struct {
	Config *config.Config
}

func NewAuthHandler(cfg *config.Config) *AuthHandler {
	return &AuthHandler{Config: cfg}
}

func controlPlaneBaseURL(cfg *config.Config) string {
	if cfg == nil {
		return ""
	}
	raw := strings.TrimSpace(cfg.ControlPlane.Domain)
	if raw == "" {
		return ""
	}
	if strings.Contains(raw, "://") {
		return strings.TrimSuffix(raw, "/")
	}
	scheme := "https"
	if raw == "localhost" || strings.HasPrefix(raw, "localhost:") || strings.HasPrefix(raw, "127.0.0.1") {
		scheme = "http"
	}
	return scheme + "://" + raw
}

func (h *AuthHandler) enqueueVerificationEmail(user *models.User) {
	if h == nil || h.Config == nil || user == nil {
		return
	}
	if user.EmailVerifiedAt != nil {
		return
	}
	to := strings.TrimSpace(user.Email)
	if to == "" || !h.Config.Email.Enabled() {
		return
	}

	token, err := models.IssueEmailVerificationToken(user.ID, 24*time.Hour)
	if err != nil {
		log.Printf("verify email token issue failed: user=%s err=%v", user.ID, err)
		return
	}
	cp := controlPlaneBaseURL(h.Config)
	verifyURL := ""
	if cp != "" {
		verifyURL = cp + "/verify?token=" + url.QueryEscape(token)
	} else {
		verifyURL = "/verify?token=" + url.QueryEscape(token)
	}
	subj, text, html := services.BuildVerifyEmail(h.Config, user, verifyURL)
	if _, err := models.EnqueueEmail("", "verify_email", to, subj, text, html); err != nil {
		log.Printf("verify email enqueue failed: user=%s err=%v", user.ID, err)
	}
}

func (h *AuthHandler) Register(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Email    string `json:"email"`
		Password string `json:"password"`
		Name     string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		utils.RespondError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Email == "" || req.Password == "" {
		utils.RespondError(w, http.StatusBadRequest, "email and password are required")
		return
	}
	if len(req.Password) < 8 {
		utils.RespondError(w, http.StatusBadRequest, "password must be at least 8 characters")
		return
	}

	email := strings.ToLower(strings.TrimSpace(req.Email))
	existing, err := models.GetUserByEmail(email)
	if err != nil {
		utils.RespondError(w, http.StatusInternalServerError, "database error")
		return
	}
	// Never reveal whether an email already exists (avoid user enumeration).
	if existing != nil {
		// If the account isn't verified yet, resend a verification link (best-effort).
		if existing.EmailVerifiedAt == nil {
			h.enqueueVerificationEmail(existing)
		}
		utils.RespondJSON(w, http.StatusCreated, map[string]string{"status": "verification_sent"})
		return
	}
	hash, err := utils.HashPassword(req.Password)
	if err != nil {
		utils.RespondError(w, http.StatusInternalServerError, "failed to hash password")
		return
	}
	username := req.Name
	if username == "" {
		username = req.Email
	}
	user := &models.User{Username: username, Email: email, PasswordHash: hash}
	if err := models.CreateUserWithPassword(user); err != nil {
		// Handle races/uniqueness without revealing whether the email exists.
		if existing, e2 := models.GetUserByEmail(email); e2 == nil && existing != nil {
			if existing.EmailVerifiedAt == nil {
				h.enqueueVerificationEmail(existing)
			}
			utils.RespondJSON(w, http.StatusCreated, map[string]string{"status": "verification_sent"})
			return
		}
		utils.RespondError(w, http.StatusInternalServerError, "failed to create user")
		return
	}
	// Auto-create a default workspace
	ws := &models.Workspace{Name: username + "'s workspace", OwnerID: user.ID, DeployPolicy: "cancel"}
	models.CreateWorkspace(ws)

	// Email verification required for email/password signups.
	h.enqueueVerificationEmail(user)
	utils.RespondJSON(w, http.StatusCreated, map[string]string{"status": "verification_sent"})
}

func (h *AuthHandler) Login(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		utils.RespondError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Email == "" || req.Password == "" {
		utils.RespondError(w, http.StatusBadRequest, "email and password are required")
		return
	}
	user, err := models.GetUserByEmail(req.Email)
	if err != nil {
		utils.RespondError(w, http.StatusInternalServerError, "database error")
		return
	}
	if user == nil || user.PasswordHash == "" {
		utils.RespondError(w, http.StatusUnauthorized, "invalid email or password")
		return
	}
	if !utils.CheckPassword(req.Password, user.PasswordHash) {
		utils.RespondError(w, http.StatusUnauthorized, "invalid email or password")
		return
	}
	if user.EmailVerifiedAt == nil {
		utils.RespondError(w, http.StatusForbidden, "email not verified")
		return
	}
	tokenStr, err := h.generateJWT(user)
	if err != nil {
		utils.RespondError(w, http.StatusInternalServerError, "failed to generate token")
		return
	}
	setSessionCookie(w, r, tokenStr, time.Duration(h.Config.JWT.Expiration)*time.Hour)
	utils.RespondJSON(w, http.StatusOK, map[string]interface{}{"user": user})
}

func (h *AuthHandler) VerifyEmail(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 32*1024)).Decode(&req); err != nil {
		utils.RespondError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if _, err := models.ConsumeEmailVerificationToken(req.Token); err != nil {
		if errors.Is(err, models.ErrInvalidEmailVerificationToken) {
			utils.RespondError(w, http.StatusBadRequest, "invalid or expired token")
			return
		}
		utils.RespondError(w, http.StatusInternalServerError, "verification failed")
		return
	}
	utils.RespondJSON(w, http.StatusOK, map[string]string{"status": "verified"})
}

func (h *AuthHandler) ResendVerification(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Email string `json:"email"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 32*1024)).Decode(&req); err != nil {
		utils.RespondError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	email := strings.ToLower(strings.TrimSpace(req.Email))
	if email == "" {
		utils.RespondError(w, http.StatusBadRequest, "email is required")
		return
	}

	// Never reveal whether the account exists.
	if u, err := models.GetUserByEmail(email); err == nil && u != nil && u.EmailVerifiedAt == nil {
		h.enqueueVerificationEmail(u)
	}
	utils.RespondJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (h *AuthHandler) GitHubRedirect(w http.ResponseWriter, r *http.Request) {
	state, err := utils.GenerateRandomString(24)
	if err != nil {
		utils.RespondError(w, http.StatusInternalServerError, "failed to generate oauth state")
		return
	}
	setOAuthStateCookie(w, r, state, 10*time.Minute)

	u, _ := url.Parse("https://github.com/login/oauth/authorize")
	q := u.Query()
	q.Set("client_id", h.Config.GitHub.ClientID)
	q.Set("redirect_uri", h.Config.GitHub.CallbackURL)
	q.Set("scope", "user:email,repo")
	q.Set("state", state)
	u.RawQuery = q.Encode()
	http.Redirect(w, r, u.String(), http.StatusTemporaryRedirect)
}

func (h *AuthHandler) GitHubCallback(w http.ResponseWriter, r *http.Request) {
	code := r.URL.Query().Get("code")
	state := strings.TrimSpace(r.URL.Query().Get("state"))
	if code == "" {
		utils.RespondError(w, http.StatusBadRequest, "missing code parameter")
		return
	}
	expectedState := readOAuthStateCookie(r)
	clearOAuthStateCookie(w, r)
	if state == "" || expectedState == "" || subtle.ConstantTimeCompare([]byte(state), []byte(expectedState)) != 1 {
		utils.RespondError(w, http.StatusUnauthorized, "invalid oauth state")
		return
	}
	accessToken, err := h.exchangeGitHubCode(code)
	if err != nil {
		utils.RespondError(w, http.StatusInternalServerError, "failed to exchange code: "+err.Error())
		return
	}
	ghUser, err := h.getGitHubUser(accessToken)
	if err != nil {
		utils.RespondError(w, http.StatusInternalServerError, "failed to get user info: "+err.Error())
		return
	}

	// /user often returns email=nil when the user has a private email; fetch it via /user/emails.
	if strings.TrimSpace(ghUser.Email) == "" {
		if email, err := h.getGitHubPrimaryEmail(accessToken); err == nil {
			ghUser.Email = strings.TrimSpace(email)
		} else {
			log.Printf("GitHub: failed to fetch user emails: %v", err)
		}
	}

	user, err := models.GetUserByGitHubID(ghUser.ID)
	if err != nil {
		utils.RespondError(w, http.StatusInternalServerError, "database error: "+err.Error())
		return
	}
	createdNew := false
	var createdWS *models.Workspace

	// If the GitHubID isn't linked yet, attempt to link by email to prevent duplicate accounts.
	if user == nil && strings.TrimSpace(ghUser.Email) != "" {
		if byEmail, err := models.GetUserByEmail(strings.TrimSpace(ghUser.Email)); err == nil && byEmail != nil && byEmail.GitHubID == 0 {
			if err := models.LinkGitHubToUser(byEmail.ID, ghUser.ID, ghUser.Login, ghUser.Email, ghUser.AvatarURL); err != nil {
				log.Printf("GitHub: failed to link user by email=%s: %v", ghUser.Email, err)
			} else {
				byEmail.GitHubID = ghUser.ID
				byEmail.Username = ghUser.Login
				byEmail.Email = ghUser.Email
				byEmail.AvatarURL = ghUser.AvatarURL
				user = byEmail
			}
		}
	}
	if user == nil {
		createdNew = true
		user = &models.User{GitHubID: ghUser.ID, Username: ghUser.Login, Email: ghUser.Email, AvatarURL: ghUser.AvatarURL}
		if err := models.CreateUser(user); err != nil {
			utils.RespondError(w, http.StatusInternalServerError, "failed to create user: "+err.Error())
			return
		}
		ws := &models.Workspace{Name: ghUser.Login + "'s workspace", OwnerID: user.ID, DeployPolicy: "cancel"}
		models.CreateWorkspace(ws)
		createdWS = ws
	} else {
		user.Username = ghUser.Login
		user.Email = ghUser.Email
		user.AvatarURL = ghUser.AvatarURL
		models.UpdateUser(user)
	}

	// Save encrypted GitHub access token for repo browsing and private clone
	if encToken, err := utils.Encrypt(accessToken, h.Config.Crypto.EncryptionKey); err == nil {
		models.UpdateUserGitHubToken(user.ID, encToken)
	}

	// Best-effort welcome email for new GitHub signups.
	if createdNew && h != nil && h.Config != nil && h.Config.Email.Enabled() && strings.TrimSpace(user.Email) != "" {
		wsName := ""
		if createdWS != nil {
			wsName = createdWS.Name
		}
		subj, text, html := services.BuildWelcomeEmail(h.Config, user, wsName)
		if _, err := models.EnqueueEmail("welcome:"+user.ID, "welcome", user.Email, subj, text, html); err != nil {
			log.Printf("email enqueue failed: type=welcome user=%s err=%v", user.ID, err)
		}
	}

	tokenStr, err := h.generateJWT(user)
	if err != nil {
		utils.RespondError(w, http.StatusInternalServerError, "failed to generate token")
		return
	}
	setSessionCookie(w, r, tokenStr, time.Duration(h.Config.JWT.Expiration)*time.Hour)
	http.Redirect(w, r, "/", http.StatusTemporaryRedirect)
}

func (h *AuthHandler) Logout(w http.ResponseWriter, r *http.Request) {
	clearSessionCookie(w, r)
	utils.RespondJSON(w, http.StatusOK, map[string]string{"status": "logged_out"})
}

func (h *AuthHandler) GetCurrentUser(w http.ResponseWriter, r *http.Request) {
	userID := middleware.GetUserID(r)
	user, err := models.GetUserByID(userID)
	if err != nil {
		utils.RespondError(w, http.StatusInternalServerError, "database error")
		return
	}
	if user == nil {
		utils.RespondError(w, http.StatusNotFound, "user not found")
		return
	}
	ws, _ := models.GetWorkspaceByOwner(userID)

	// Expose whether the user has linked a GitHub account (token is json:"-").
	githubConnected := user.GitHubAccessToken != ""

	utils.RespondJSON(w, http.StatusOK, map[string]interface{}{
		"user":             user,
		"workspace":        ws,
		"github_connected": githubConnected,
	})
}

func (h *AuthHandler) GetBlueprintAISettings(w http.ResponseWriter, r *http.Request) {
	userID := middleware.GetUserID(r)
	user, err := models.GetUserByID(userID)
	if err != nil {
		utils.RespondError(w, http.StatusInternalServerError, "database error")
		return
	}
	if user == nil {
		utils.RespondError(w, http.StatusNotFound, "user not found")
		return
	}
	available := h != nil &&
		h.Config != nil &&
		h.Config.BlueprintAI.Enabled &&
		strings.TrimSpace(h.Config.BlueprintAI.OpenRouterAPIKey) != ""
	model := ""
	if h != nil && h.Config != nil {
		model = strings.TrimSpace(h.Config.BlueprintAI.OpenRouterModel)
	}
	utils.RespondJSON(w, http.StatusOK, map[string]interface{}{
		"enabled":   user.BlueprintAIAutogenEnabled,
		"available": available,
		"model":     model,
	})
}

func (h *AuthHandler) UpdateBlueprintAISettings(w http.ResponseWriter, r *http.Request) {
	userID := middleware.GetUserID(r)
	var req struct {
		Enabled bool `json:"enabled"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 32*1024)).Decode(&req); err != nil {
		utils.RespondError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	available := h != nil &&
		h.Config != nil &&
		h.Config.BlueprintAI.Enabled &&
		strings.TrimSpace(h.Config.BlueprintAI.OpenRouterAPIKey) != ""
	if req.Enabled && !available {
		utils.RespondError(w, http.StatusServiceUnavailable, "blueprint ai is not configured")
		return
	}
	if err := models.UpdateUserBlueprintAIAutogen(userID, req.Enabled); err != nil {
		utils.RespondError(w, http.StatusInternalServerError, "failed to update settings")
		return
	}
	model := ""
	if h != nil && h.Config != nil {
		model = strings.TrimSpace(h.Config.BlueprintAI.OpenRouterModel)
	}
	utils.RespondJSON(w, http.StatusOK, map[string]interface{}{
		"enabled":   req.Enabled,
		"available": available,
		"model":     model,
	})
}

func (h *AuthHandler) CreateAPIKey(w http.ResponseWriter, r *http.Request) {
	userID := middleware.GetUserID(r)
	var req struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		utils.RespondError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	rawKey, err := utils.GenerateRandomString(32)
	if err != nil {
		utils.RespondError(w, http.StatusInternalServerError, "failed to generate key")
		return
	}
	hash, err := utils.HashAPIKey(rawKey)
	if err != nil {
		utils.RespondError(w, http.StatusInternalServerError, "failed to hash key")
		return
	}
	key := &models.APIKey{UserID: userID, Name: req.Name, KeyHash: hash}
	if err := models.CreateAPIKey(key); err != nil {
		utils.RespondError(w, http.StatusInternalServerError, "failed to create api key")
		return
	}
	utils.RespondJSON(w, http.StatusCreated, map[string]interface{}{"id": key.ID, "key": rawKey, "name": key.Name})
}

func (h *AuthHandler) DeleteAPIKey(w http.ResponseWriter, r *http.Request) {
	userID := middleware.GetUserID(r)
	keyID := mux.Vars(r)["id"]
	if err := models.DeleteAPIKey(keyID, userID); err != nil {
		utils.RespondError(w, http.StatusInternalServerError, "failed to delete api key")
		return
	}
	utils.RespondJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

type gitHubTokenResponse struct {
	AccessToken string `json:"access_token"`
}

type gitHubUser struct {
	ID        int64  `json:"id"`
	Login     string `json:"login"`
	Email     string `json:"email"`
	AvatarURL string `json:"avatar_url"`
}

func (h *AuthHandler) exchangeGitHubCode(code string) (string, error) {
	url := fmt.Sprintf("https://github.com/login/oauth/access_token?client_id=%s&client_secret=%s&code=%s",
		h.Config.GitHub.ClientID, h.Config.GitHub.ClientSecret, code)
	req, _ := http.NewRequest("POST", url, nil)
	req.Header.Set("Accept", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("github token exchange failed (%d): %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	var tokenResp gitHubTokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&tokenResp); err != nil {
		return "", err
	}
	return tokenResp.AccessToken, nil
}

func (h *AuthHandler) getGitHubUser(token string) (*gitHubUser, error) {
	req, _ := http.NewRequest("GET", "https://api.github.com/user", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/vnd.github+json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("github user fetch failed (%d): %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	body, _ := io.ReadAll(resp.Body)
	var user gitHubUser
	if err := json.Unmarshal(body, &user); err != nil {
		return nil, err
	}
	return &user, nil
}

type gitHubEmail struct {
	Email      string `json:"email"`
	Primary    bool   `json:"primary"`
	Verified   bool   `json:"verified"`
	Visibility string `json:"visibility"`
}

func (h *AuthHandler) getGitHubPrimaryEmail(token string) (string, error) {
	req, _ := http.NewRequest("GET", "https://api.github.com/user/emails", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/vnd.github+json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("github emails fetch failed (%d): %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var emails []gitHubEmail
	if err := json.NewDecoder(resp.Body).Decode(&emails); err != nil {
		return "", err
	}

	// Prefer primary + verified; fall back to anything usable.
	for _, e := range emails {
		if e.Primary && e.Verified && strings.TrimSpace(e.Email) != "" {
			return e.Email, nil
		}
	}
	for _, e := range emails {
		if e.Primary && strings.TrimSpace(e.Email) != "" {
			return e.Email, nil
		}
	}
	for _, e := range emails {
		if e.Verified && strings.TrimSpace(e.Email) != "" {
			return e.Email, nil
		}
	}
	for _, e := range emails {
		if strings.TrimSpace(e.Email) != "" {
			return e.Email, nil
		}
	}
	return "", nil
}

func (h *AuthHandler) generateJWT(user *models.User) (string, error) {
	claims := jwt.RegisteredClaims{
		Subject:   user.ID,
		ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Duration(h.Config.JWT.Expiration) * time.Hour)),
		IssuedAt:  jwt.NewNumericDate(time.Now()),
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString([]byte(h.Config.JWT.Secret))
}
