package database

import (
	"database/sql"
	"fmt"
	"log"

	_ "github.com/lib/pq"
	"github.com/railpush/api/config"
)

var DB *sql.DB

func Connect(cfg *config.DatabaseConfig) error {
	var err error
	DB, err = sql.Open("postgres", cfg.DSN())
	if err != nil {
		return fmt.Errorf("failed to open database: %w", err)
	}
	DB.SetMaxOpenConns(25)
	DB.SetMaxIdleConns(5)
	if err = DB.Ping(); err != nil {
		return fmt.Errorf("failed to ping database: %w", err)
	}
	log.Println("Connected to PostgreSQL")
	return nil
}

func Close() {
	if DB != nil {
		DB.Close()
	}
}
