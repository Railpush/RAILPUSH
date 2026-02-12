package models

import (
	"database/sql"
	"time"

	"github.com/railpush/api/database"
)

type User struct {
	ID                string    `json:"id"`
	GitHubID          int64     `json:"github_id"`
	Username          string    `json:"username"`
	Email             string    `json:"email"`
	PasswordHash      string    `json:"-"`
	AvatarURL         string    `json:"avatar_url"`
	Role              string    `json:"role"`
	GitHubAccessToken string    `json:"-"`
	CreatedAt         time.Time `json:"created_at"`
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

func GetUserByGitHubID(ghID int64) (*User, error) {
	u := &User{}
	err := database.DB.QueryRow(
		"SELECT id, COALESCE(github_id, 0), COALESCE(username, ''), COALESCE(email, ''), COALESCE(avatar_url, ''), COALESCE(role, 'member'), created_at FROM users WHERE github_id = $1", ghID,
	).Scan(&u.ID, &u.GitHubID, &u.Username, &u.Email, &u.AvatarURL, &u.Role, &u.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return u, err
}

func GetUserByID(id string) (*User, error) {
	u := &User{}
	err := database.DB.QueryRow(
		"SELECT id, COALESCE(github_id, 0), COALESCE(username, ''), COALESCE(email, ''), COALESCE(avatar_url, ''), COALESCE(role, 'member'), created_at FROM users WHERE id = $1", id,
	).Scan(&u.ID, &u.GitHubID, &u.Username, &u.Email, &u.AvatarURL, &u.Role, &u.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return u, err
}

func CreateUser(u *User) error {
	return database.DB.QueryRow(
		"INSERT INTO users (github_id, username, email, avatar_url) VALUES ($1, $2, $3, $4) RETURNING id, created_at",
		u.GitHubID, u.Username, u.Email, u.AvatarURL,
	).Scan(&u.ID, &u.CreatedAt)
}

func GetUserByEmail(email string) (*User, error) {
	u := &User{}
	err := database.DB.QueryRow(
		"SELECT id, COALESCE(github_id, 0), COALESCE(username, ''), email, COALESCE(password_hash, ''), COALESCE(avatar_url, ''), COALESCE(role, 'member'), created_at FROM users WHERE email = $1", email,
	).Scan(&u.ID, &u.GitHubID, &u.Username, &u.Email, &u.PasswordHash, &u.AvatarURL, &u.Role, &u.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return u, err
}

func CreateUserWithPassword(u *User) error {
	return database.DB.QueryRow(
		"INSERT INTO users (username, email, password_hash) VALUES ($1, $2, $3) RETURNING id, role, created_at",
		u.Username, u.Email, u.PasswordHash,
	).Scan(&u.ID, &u.Role, &u.CreatedAt)
}

func UpdateUser(u *User) error {
	_, err := database.DB.Exec(
		"UPDATE users SET username=$1, email=$2, avatar_url=$3 WHERE id=$4",
		u.Username, u.Email, u.AvatarURL, u.ID,
	)
	return err
}

func CreateAPIKey(k *APIKey) error {
	return database.DB.QueryRow(
		"INSERT INTO api_keys (user_id, name, key_hash, expires_at) VALUES ($1, $2, $3, $4) RETURNING id, created_at",
		k.UserID, k.Name, k.KeyHash, k.ExpiresAt,
	).Scan(&k.ID, &k.CreatedAt)
}

func DeleteAPIKey(id, userID string) error {
	_, err := database.DB.Exec("DELETE FROM api_keys WHERE id=$1 AND user_id=$2", id, userID)
	return err
}

func ListAPIKeys(userID string) ([]APIKey, error) {
	rows, err := database.DB.Query("SELECT id, user_id, name, key_hash, expires_at, created_at FROM api_keys WHERE user_id=$1", userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var keys []APIKey
	for rows.Next() {
		var k APIKey
		if err := rows.Scan(&k.ID, &k.UserID, &k.Name, &k.KeyHash, &k.ExpiresAt, &k.CreatedAt); err != nil {
			return nil, err
		}
		keys = append(keys, k)
	}
	return keys, nil
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
