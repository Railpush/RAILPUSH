package utils

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"golang.org/x/crypto/bcrypt"
)

const errorDocsBaseURL = "https://docs.railpush.com/api/errors#"

var nonErrorCodeChars = regexp.MustCompile(`[^A-Z0-9]+`)

type ValidationIssue struct {
	Field   string `json:"field"`
	Message string `json:"message"`
}

type ErrorOptions struct {
	Code       string            `json:"error_code"`
	Message    string            `json:"message"`
	Suggestion string            `json:"suggestion,omitempty"`
	DocsURL    string            `json:"docs_url,omitempty"`
	Errors     []ValidationIssue `json:"errors,omitempty"`
}

type ErrorResponse struct {
	ErrorCode  string            `json:"error_code"`
	Message    string            `json:"message"`
	Error      string            `json:"error"`
	Suggestion string            `json:"suggestion,omitempty"`
	DocsURL    string            `json:"docs_url,omitempty"`
	Errors     []ValidationIssue `json:"errors,omitempty"`
}

func GenerateRandomString(length int) (string, error) {
	bytes := make([]byte, length)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return hex.EncodeToString(bytes), nil
}

func HashPassword(password string) (string, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return "", err
	}
	return string(hash), nil
}

func CheckPassword(password, hash string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) == nil
}

func HashAPIKey(key string) (string, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(key), bcrypt.DefaultCost)
	if err != nil {
		return "", err
	}
	return string(hash), nil
}

func CheckAPIKeyHash(key, hash string) bool {
	err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(key))
	return err == nil
}

func RespondJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if data != nil {
		json.NewEncoder(w).Encode(data)
	}
}

func RespondError(w http.ResponseWriter, status int, message string) {
	RespondErrorWithOptions(w, status, ErrorOptions{
		Code:    inferErrorCode(status, message),
		Message: message,
	})
}

func RespondErrorWithCode(w http.ResponseWriter, status int, code string, message string) {
	RespondErrorWithOptions(w, status, ErrorOptions{
		Code:    code,
		Message: message,
	})
}

func RespondErrorWithSuggestion(w http.ResponseWriter, status int, code string, message string, suggestion string) {
	RespondErrorWithOptions(w, status, ErrorOptions{
		Code:       code,
		Message:    message,
		Suggestion: suggestion,
	})
}

func RespondValidationErrors(w http.ResponseWriter, status int, issues []ValidationIssue) {
	if status <= 0 {
		status = http.StatusBadRequest
	}
	cleaned := make([]ValidationIssue, 0, len(issues))
	for _, issue := range issues {
		field := strings.TrimSpace(issue.Field)
		if field == "" {
			field = "request"
		}
		msg := strings.TrimSpace(issue.Message)
		if msg == "" {
			msg = "is invalid"
		}
		cleaned = append(cleaned, ValidationIssue{Field: field, Message: msg})
	}
	if len(cleaned) == 0 {
		cleaned = append(cleaned, ValidationIssue{Field: "request", Message: "validation failed"})
	}
	RespondErrorWithOptions(w, status, ErrorOptions{
		Code:    "VALIDATION_ERROR",
		Message: "validation failed",
		Errors:  cleaned,
	})
}

func RespondErrorWithOptions(w http.ResponseWriter, status int, opts ErrorOptions) {
	msg := strings.TrimSpace(opts.Message)
	if msg == "" {
		msg = http.StatusText(status)
		if msg == "" {
			msg = "request failed"
		}
	}

	code := sanitizeErrorCode(opts.Code)
	if code == "" {
		code = inferErrorCode(status, msg)
	}
	if code == "" {
		code = httpStatusErrorCode(status)
	}
	if code == "" {
		code = "ERROR"
	}

	docsURL := strings.TrimSpace(opts.DocsURL)
	if docsURL == "" {
		docsURL = errorDocsBaseURL + code
	}

	resp := ErrorResponse{
		ErrorCode:  code,
		Message:    msg,
		Error:      msg,
		Suggestion: strings.TrimSpace(opts.Suggestion),
		DocsURL:    docsURL,
		Errors:     opts.Errors,
	}
	RespondJSON(w, status, resp)
}

func inferErrorCode(status int, message string) string {
	lower := strings.ToLower(strings.TrimSpace(message))
	switch {
	case strings.Contains(lower, "validation"):
		return "VALIDATION_ERROR"
	case strings.Contains(lower, "invalid request body"):
		return "INVALID_REQUEST_BODY"
	case strings.Contains(lower, "not found"):
		if strings.Contains(lower, "service") {
			return "SERVICE_NOT_FOUND"
		}
		if strings.Contains(lower, "database") {
			return "DATABASE_NOT_FOUND"
		}
		if strings.Contains(lower, "key") {
			return "KEYVALUE_NOT_FOUND"
		}
		return "NOT_FOUND"
	case strings.Contains(lower, "forbidden"):
		return "FORBIDDEN"
	case strings.Contains(lower, "unauthorized"):
		return "UNAUTHORIZED"
	case strings.Contains(lower, "confirmation token expired"):
		return "CONFIRMATION_TOKEN_EXPIRED"
	case strings.Contains(lower, "invalid confirmation token"):
		return "INVALID_CONFIRMATION_TOKEN"
	}

	if status >= http.StatusInternalServerError {
		if code := httpStatusErrorCode(status); code != "" {
			return code
		}
	}

	if code := sanitizeErrorCode(message); code != "" && code != "ERROR" {
		return code
	}
	return httpStatusErrorCode(status)
}

func httpStatusErrorCode(status int) string {
	switch status {
	case http.StatusBadRequest:
		return "BAD_REQUEST"
	case http.StatusUnauthorized:
		return "UNAUTHORIZED"
	case http.StatusForbidden:
		return "FORBIDDEN"
	case http.StatusNotFound:
		return "NOT_FOUND"
	case http.StatusConflict:
		return "CONFLICT"
	case http.StatusUnprocessableEntity:
		return "VALIDATION_ERROR"
	case http.StatusTooManyRequests:
		return "RATE_LIMITED"
	case http.StatusInternalServerError:
		return "INTERNAL_ERROR"
	case http.StatusBadGateway:
		return "BAD_GATEWAY"
	case http.StatusServiceUnavailable:
		return "SERVICE_UNAVAILABLE"
	default:
		return ""
	}
}

func sanitizeErrorCode(raw string) string {
	code := strings.ToUpper(strings.TrimSpace(raw))
	if code == "" {
		return ""
	}
	code = nonErrorCodeChars.ReplaceAllString(code, "_")
	code = strings.Trim(code, "_")
	if code == "" {
		return ""
	}
	if len(code) > 64 {
		code = code[:64]
	}
	return code
}

func ParseID(r *http.Request, key string) string {
	parts := strings.Split(r.URL.Path, "/")
	for i, p := range parts {
		if p == key && i+1 < len(parts) {
			return parts[i+1]
		}
	}
	return ""
}

func GetQueryInt(r *http.Request, key string, defaultVal int) int {
	v := r.URL.Query().Get(key)
	if v == "" {
		return defaultVal
	}
	i, err := strconv.Atoi(v)
	if err != nil {
		return defaultVal
	}
	return i
}

func GetQueryString(r *http.Request, key string, defaultVal string) string {
	v := r.URL.Query().Get(key)
	if v == "" {
		return defaultVal
	}
	return v
}
