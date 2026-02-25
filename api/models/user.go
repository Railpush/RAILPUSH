package models

import (
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/lib/pq"
	"github.com/railpush/api/database"
	"golang.org/x/crypto/bcrypt"
)

type User struct {
	ID                        string     `json:"id"`
	GitHubID                  int64      `json:"github_id"`
	Username                  string     `json:"username"`
	Email                     string     `json:"email"`
	EmailVerifiedAt           *time.Time `json:"email_verified_at,omitempty"`
	PasswordHash              string     `json:"-"`
	AvatarURL                 string     `json:"avatar_url"`
	Role                      string     `json:"role"`
	IsSuspended               bool       `json:"is_suspended"`
	SuspendedAt               *time.Time `json:"suspended_at,omitempty"`
	BlueprintAIAutogenEnabled bool       `json:"blueprint_ai_autogen_enabled"`
	GitHubAccessToken         string     `json:"-"`
	CreatedAt                 time.Time  `json:"created_at"`
}

type APIKey struct {
	ID        string     `json:"id"`
	UserID    string     `json:"user_id"`
	Name      string     `json:"name"`
	KeyHash   string     `json:"-"`
	Scopes    []string   `json:"scopes"`
	ExpiresAt *time.Time `json:"expires_at"`
	CreatedAt time.Time  `json:"created_at"`
}

const (
	APIKeyScopeAll     = "*"
	APIKeyScopeRead    = "read"
	APIKeyScopeWrite   = "write"
	APIKeyScopeDeploy  = "deploy"
	APIKeyScopeSupport = "support"
	APIKeyScopeOps     = "ops"
	APIKeyScopeBilling = "billing"
	APIKeyScopeAdmin   = "admin"
)

var validAPIKeyScopes = map[string]struct{}{
	APIKeyScopeAll:     {},
	APIKeyScopeRead:    {},
	APIKeyScopeWrite:   {},
	APIKeyScopeDeploy:  {},
	APIKeyScopeSupport: {},
	APIKeyScopeOps:     {},
	APIKeyScopeBilling: {},
	APIKeyScopeAdmin:   {},
}

var DefaultAPIKeyScopes = []string{APIKeyScopeRead, APIKeyScopeWrite, APIKeyScopeDeploy}

type ResolvedAPIKey struct {
	ID        string
	UserID    string
	Scopes    []string
	ExpiresAt *time.Time
}

func NormalizeAndValidateAPIKeyScopes(raw []string) ([]string, error) {
	if len(raw) == 0 {
		return append([]string{}, DefaultAPIKeyScopes...), nil
	}
	seen := map[string]struct{}{}
	out := make([]string, 0, len(raw))
	for _, s := range raw {
		norm := strings.ToLower(strings.TrimSpace(s))
		if norm == "" {
			continue
		}
		if _, ok := validAPIKeyScopes[norm]; !ok {
			return nil, fmt.Errorf("invalid scope %q", s)
		}
		if norm == APIKeyScopeAll {
			return []string{APIKeyScopeAll}, nil
		}
		if _, ok := seen[norm]; ok {
			continue
		}
		seen[norm] = struct{}{}
		out = append(out, norm)
	}
	if len(out) == 0 {
		return append([]string{}, DefaultAPIKeyScopes...), nil
	}
	return out, nil
}

func HasAnyAPIKeyScope(granted []string, required ...string) bool {
	normGranted := map[string]struct{}{}
	for _, s := range granted {
		norm := strings.ToLower(strings.TrimSpace(s))
		if norm == "" {
			continue
		}
		normGranted[norm] = struct{}{}
	}
	if _, ok := normGranted[APIKeyScopeAll]; ok {
		return true
	}
	for _, s := range required {
		norm := strings.ToLower(strings.TrimSpace(s))
		if norm == "" {
			continue
		}
		if _, ok := normGranted[norm]; ok {
			return true
		}
	}
	return false
}

func GetUserByGitHubID(ghID int64) (*User, error) {
	u := &User{}
	var verifiedAt sql.NullTime
	var suspendedAt sql.NullTime
	err := database.DB.QueryRow(
		"SELECT id, COALESCE(github_id, 0), COALESCE(username, ''), COALESCE(email, ''), email_verified_at, COALESCE(avatar_url, ''), COALESCE(role, 'member'), COALESCE(is_suspended,false), suspended_at, COALESCE(blueprint_ai_autogen_enabled, false), created_at FROM users WHERE github_id = $1", ghID,
	).Scan(&u.ID, &u.GitHubID, &u.Username, &u.Email, &verifiedAt, &u.AvatarURL, &u.Role, &u.IsSuspended, &suspendedAt, &u.BlueprintAIAutogenEnabled, &u.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if verifiedAt.Valid {
		v := verifiedAt.Time
		u.EmailVerifiedAt = &v
	}
	if suspendedAt.Valid {
		v := suspendedAt.Time
		u.SuspendedAt = &v
	}
	return u, err
}

func GetUserByID(id string) (*User, error) {
	u := &User{}
	var verifiedAt sql.NullTime
	var suspendedAt sql.NullTime
	err := database.DB.QueryRow(
		"SELECT id, COALESCE(github_id, 0), COALESCE(username, ''), COALESCE(email, ''), email_verified_at, COALESCE(avatar_url, ''), COALESCE(role, 'member'), COALESCE(is_suspended,false), suspended_at, COALESCE(blueprint_ai_autogen_enabled, false), created_at FROM users WHERE id = $1", id,
	).Scan(&u.ID, &u.GitHubID, &u.Username, &u.Email, &verifiedAt, &u.AvatarURL, &u.Role, &u.IsSuspended, &suspendedAt, &u.BlueprintAIAutogenEnabled, &u.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if verifiedAt.Valid {
		v := verifiedAt.Time
		u.EmailVerifiedAt = &v
	}
	if suspendedAt.Valid {
		v := suspendedAt.Time
		u.SuspendedAt = &v
	}
	return u, err
}

func CreateUser(u *User) error {
	return database.DB.QueryRow(
		// Bootstrap: first user becomes a platform admin (useful for self-hosted installs).
		"INSERT INTO users (github_id, username, email, email_verified_at, avatar_url, role) VALUES ($1, $2, NULLIF($3,''), NOW(), $4, (CASE WHEN (SELECT COUNT(*) FROM users)=0 THEN 'admin' ELSE 'member' END)) RETURNING id, role, created_at",
		u.GitHubID, u.Username, u.Email, u.AvatarURL,
	).Scan(&u.ID, &u.Role, &u.CreatedAt)
}

func GetUserByEmail(email string) (*User, error) {
	u := &User{}
	var verifiedAt sql.NullTime
	var suspendedAt sql.NullTime
	err := database.DB.QueryRow(
		"SELECT id, COALESCE(github_id, 0), COALESCE(username, ''), email, email_verified_at, COALESCE(password_hash, ''), COALESCE(avatar_url, ''), COALESCE(role, 'member'), COALESCE(is_suspended,false), suspended_at, COALESCE(blueprint_ai_autogen_enabled, false), created_at FROM users WHERE email = $1", email,
	).Scan(&u.ID, &u.GitHubID, &u.Username, &u.Email, &verifiedAt, &u.PasswordHash, &u.AvatarURL, &u.Role, &u.IsSuspended, &suspendedAt, &u.BlueprintAIAutogenEnabled, &u.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if verifiedAt.Valid {
		v := verifiedAt.Time
		u.EmailVerifiedAt = &v
	}
	if suspendedAt.Valid {
		v := suspendedAt.Time
		u.SuspendedAt = &v
	}
	return u, err
}

func CreateUserWithPassword(u *User) error {
	return database.DB.QueryRow(
		// Bootstrap: first user becomes a platform admin (useful for self-hosted installs).
		"INSERT INTO users (username, email, password_hash, role, email_verified_at) VALUES ($1, $2, $3, (CASE WHEN (SELECT COUNT(*) FROM users)=0 THEN 'admin' ELSE 'member' END), NULL) RETURNING id, role, created_at",
		u.Username, u.Email, u.PasswordHash,
	).Scan(&u.ID, &u.Role, &u.CreatedAt)
}

func UpdateUser(u *User) error {
	_, err := database.DB.Exec(
		"UPDATE users SET username=$1, email=NULLIF($2,''), avatar_url=$3 WHERE id=$4",
		u.Username, u.Email, u.AvatarURL, u.ID,
	)
	return err
}

// LinkGitHubToUser attaches a GitHub identity to an existing (non-GitHub) user.
// This avoids duplicate accounts when a user signs up with email/password then connects GitHub.
func LinkGitHubToUser(userID string, githubID int64, username string, email string, avatarURL string) error {
	_, err := database.DB.Exec(
		"UPDATE users SET github_id=$1, username=$2, email=NULLIF($3,''), avatar_url=$4, email_verified_at=COALESCE(email_verified_at, NOW()) WHERE id=$5 AND (github_id IS NULL OR github_id=0)",
		githubID, username, email, avatarURL, userID,
	)
	return err
}

func CreateAPIKey(k *APIKey) error {
	if k == nil {
		return fmt.Errorf("missing api key")
	}
	scopes, err := NormalizeAndValidateAPIKeyScopes(k.Scopes)
	if err != nil {
		return err
	}
	k.Scopes = scopes
	return database.DB.QueryRow(
		"INSERT INTO api_keys (user_id, name, key_hash, scopes, expires_at) VALUES ($1, $2, $3, $4, $5) RETURNING id, created_at",
		k.UserID, k.Name, k.KeyHash, pq.Array(k.Scopes), k.ExpiresAt,
	).Scan(&k.ID, &k.CreatedAt)
}

func DeleteAPIKey(id, userID string) error {
	_, err := database.DB.Exec("DELETE FROM api_keys WHERE id=$1 AND user_id=$2", id, userID)
	return err
}

func ListAPIKeys(userID string) ([]APIKey, error) {
	rows, err := database.DB.Query("SELECT id, user_id, name, key_hash, scopes, expires_at, created_at FROM api_keys WHERE user_id=$1", userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var keys []APIKey
	for rows.Next() {
		var k APIKey
		var scopes pq.StringArray
		if err := rows.Scan(&k.ID, &k.UserID, &k.Name, &k.KeyHash, &scopes, &k.ExpiresAt, &k.CreatedAt); err != nil {
			return nil, err
		}
		if len(scopes) == 0 {
			k.Scopes = []string{APIKeyScopeAll}
		} else {
			k.Scopes = []string(scopes)
		}
		keys = append(keys, k)
	}
	return keys, nil
}

// ResolveAPIKey checks a raw API key against stored hashes and returns
// key identity + normalized scopes on match. Returns (nil, nil) when no match.
func ResolveAPIKey(rawKey string) (*ResolvedAPIKey, error) {
	rows, err := database.DB.Query(
		"SELECT id, user_id, key_hash, scopes, expires_at FROM api_keys",
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	now := time.Now()
	for rows.Next() {
		var id, userID, hash string
		var scopes pq.StringArray
		var expiresAt sql.NullTime
		if err := rows.Scan(&id, &userID, &hash, &scopes, &expiresAt); err != nil {
			return nil, err
		}
		if expiresAt.Valid && expiresAt.Time.Before(now) {
			continue // expired
		}
		if bcrypt.CompareHashAndPassword([]byte(hash), []byte(rawKey)) == nil {
			normalizedScopes := []string{APIKeyScopeAll}
			if len(scopes) > 0 {
				normalized, nerr := NormalizeAndValidateAPIKeyScopes([]string(scopes))
				if nerr == nil && len(normalized) > 0 {
					normalizedScopes = normalized
				}
			}
			var expPtr *time.Time
			if expiresAt.Valid {
				exp := expiresAt.Time
				expPtr = &exp
			}
			return &ResolvedAPIKey{ID: id, UserID: userID, Scopes: normalizedScopes, ExpiresAt: expPtr}, nil
		}
	}
	return nil, nil
}

func GetUserGitHubToken(userID string) (string, error) {
	var token sql.NullString
	err := database.DB.QueryRow("SELECT github_access_token FROM users WHERE id = $1", userID).Scan(&token)
	if err != nil {
		return "", err
	}
	return token.String, nil
}

func UpdateUserGitHubToken(userID, encryptedToken string) error {
	_, err := database.DB.Exec("UPDATE users SET github_access_token = $1 WHERE id = $2", encryptedToken, userID)
	return err
}

func UpdateUserBlueprintAIAutogen(userID string, enabled bool) error {
	_, err := database.DB.Exec("UPDATE users SET blueprint_ai_autogen_enabled = $1 WHERE id = $2", enabled, userID)
	return err
}
