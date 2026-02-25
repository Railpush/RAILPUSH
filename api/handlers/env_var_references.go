package handlers

import (
	"fmt"
	"net"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"github.com/railpush/api/config"
	"github.com/railpush/api/models"
	"github.com/railpush/api/utils"
)

var databaseTemplateReferencePattern = regexp.MustCompile(`\$\{\{\s*databases\.([A-Za-z0-9._:-]+)\.([A-Za-z0-9_:-]+)\s*\}\}`)

func resolveDatabaseTemplateReferences(cfg *config.Config, workspaceID, raw string) (string, error) {
	if !databaseTemplateReferencePattern.MatchString(raw) {
		return raw, nil
	}
	workspaceID = strings.TrimSpace(workspaceID)
	if workspaceID == "" {
		return "", fmt.Errorf("workspace not found for database references")
	}

	dbs, err := models.ListManagedDatabasesByWorkspace(workspaceID)
	if err != nil {
		return "", fmt.Errorf("failed to load databases for workspace")
	}

	byID := make(map[string]models.ManagedDatabase, len(dbs))
	byName := make(map[string]models.ManagedDatabase, len(dbs))
	ambiguousNames := map[string]struct{}{}

	for _, db := range dbs {
		if strings.EqualFold(strings.TrimSpace(db.Status), "soft_deleted") {
			continue
		}
		idKey := strings.ToLower(strings.TrimSpace(db.ID))
		if idKey != "" {
			byID[idKey] = db
		}
		nameKey := strings.ToLower(strings.TrimSpace(db.Name))
		if nameKey == "" {
			continue
		}
		if _, exists := byName[nameKey]; exists {
			delete(byName, nameKey)
			ambiguousNames[nameKey] = struct{}{}
			continue
		}
		if _, ambiguous := ambiguousNames[nameKey]; ambiguous {
			continue
		}
		byName[nameKey] = db
	}

	fullCache := map[string]*models.ManagedDatabase{}
	passwordCache := map[string]string{}

	getFullDatabase := func(db models.ManagedDatabase) (*models.ManagedDatabase, error) {
		key := strings.TrimSpace(db.ID)
		if cached, ok := fullCache[key]; ok {
			return cached, nil
		}
		fresh, err := models.GetManagedDatabase(key)
		if err != nil || fresh == nil {
			return nil, fmt.Errorf("failed to load database %q", db.Name)
		}
		fullCache[key] = fresh
		return fresh, nil
	}

	getDatabasePassword := func(db models.ManagedDatabase) (string, error) {
		key := strings.TrimSpace(db.ID)
		if cached, ok := passwordCache[key]; ok {
			return cached, nil
		}
		if cfg == nil {
			return "", fmt.Errorf("database reference decryption is unavailable")
		}
		fresh, err := getFullDatabase(db)
		if err != nil {
			return "", err
		}
		encryptionKey := strings.TrimSpace(cfg.Crypto.EncryptionKey)
		if encryptionKey == "" {
			return "", fmt.Errorf("database reference decryption is unavailable")
		}
		if strings.TrimSpace(fresh.EncryptedPassword) == "" {
			return "", fmt.Errorf("database %q credentials are not ready", db.Name)
		}
		pw, err := utils.Decrypt(fresh.EncryptedPassword, encryptionKey)
		if err != nil || strings.TrimSpace(pw) == "" {
			return "", fmt.Errorf("failed to decrypt credentials for database %q", db.Name)
		}
		passwordCache[key] = pw
		return pw, nil
	}

	resolveDatabase := func(reference string) (models.ManagedDatabase, error) {
		key := strings.ToLower(strings.TrimSpace(reference))
		if key == "" {
			return models.ManagedDatabase{}, fmt.Errorf("empty database reference")
		}
		if db, ok := byID[key]; ok {
			return db, nil
		}
		if _, ambiguous := ambiguousNames[key]; ambiguous {
			return models.ManagedDatabase{}, fmt.Errorf("database reference %q is ambiguous; use the database ID", reference)
		}
		if db, ok := byName[key]; ok {
			return db, nil
		}
		return models.ManagedDatabase{}, fmt.Errorf("unknown database reference %q", reference)
	}

	resolveField := func(db models.ManagedDatabase, field string) (string, error) {
		field = strings.ToLower(strings.TrimSpace(field))
		host := strings.TrimSpace(db.Host)
		port := db.Port
		if port <= 0 {
			port = 5432
		}
		dbName := strings.TrimSpace(db.DBName)
		if dbName == "" {
			dbName = strings.TrimSpace(db.Name)
		}
		username := strings.TrimSpace(db.Username)
		if username == "" {
			username = strings.TrimSpace(db.Name)
		}

		switch field {
		case "host":
			if host == "" {
				return "", fmt.Errorf("database %q host is not ready", db.Name)
			}
			return host, nil
		case "port":
			return strconv.Itoa(port), nil
		case "db_name", "database":
			if dbName == "" {
				return "", fmt.Errorf("database %q name is not ready", db.Name)
			}
			return dbName, nil
		case "username", "user":
			if username == "" {
				return "", fmt.Errorf("database %q username is not ready", db.Name)
			}
			return username, nil
		case "password":
			return getDatabasePassword(db)
		case "internal_url":
			if host == "" {
				return "", fmt.Errorf("database %q host is not ready", db.Name)
			}
			if dbName == "" {
				return "", fmt.Errorf("database %q name is not ready", db.Name)
			}
			if username == "" {
				return "", fmt.Errorf("database %q username is not ready", db.Name)
			}
			password, err := getDatabasePassword(db)
			if err != nil {
				return "", err
			}
			u := &url.URL{
				Scheme: "postgresql",
				User:   url.UserPassword(username, password),
				Host:   net.JoinHostPort(host, strconv.Itoa(port)),
				Path:   "/" + dbName,
			}
			return u.String(), nil
		default:
			return "", fmt.Errorf("unsupported database reference field %q (supported: internal_url, host, port, db_name, username, password)", field)
		}
	}

	matches := databaseTemplateReferencePattern.FindAllStringSubmatchIndex(raw, -1)
	if len(matches) == 0 {
		return raw, nil
	}

	var out strings.Builder
	last := 0
	for _, m := range matches {
		out.WriteString(raw[last:m[0]])
		reference := raw[m[2]:m[3]]
		field := raw[m[4]:m[5]]
		db, err := resolveDatabase(reference)
		if err != nil {
			return "", err
		}
		value, err := resolveField(db, field)
		if err != nil {
			return "", err
		}
		out.WriteString(value)
		last = m[1]
	}
	out.WriteString(raw[last:])
	return out.String(), nil
}
