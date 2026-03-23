package storage

import (
	"fmt"
	"log"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/database/sqlite3"
	_ "github.com/golang-migrate/migrate/v4/source/file"

	"insights-scheduler/internal/config"
)

type loggerWrapper struct {
	*log.Logger
}

func (lw loggerWrapper) Verbose() bool {
	return true
}

func CreateMigration(cfg *config.Config) (*migrate.Migrate, error) {
	var databaseURL string

	switch cfg.Database.Type {
	case "sqlite":
		databaseURL = fmt.Sprintf("sqlite3://%s", cfg.Database.Path)
		log.Printf("Running migrations for SQLite database: %s", cfg.Database.Path)
	case "postgres", "postgresql":

		connStr, err := buildConnectionString(cfg)
		if err != nil {
			return nil, err
		}

		databaseURL = connStr

		// Log connection info WITHOUT password
		log.Printf("Running migrations for PostgreSQL database: %s@%s:%d/%s?sslmode=%s",
			cfg.Database.Username, cfg.Database.Host, cfg.Database.Port, cfg.Database.Name, cfg.Database.SSLMode)
	default:
		return nil, fmt.Errorf("unsupported database type: %s", cfg.Database.Type)
	}

	migrationsPath := "file://db/migrations"
	m, err := migrate.New(migrationsPath, databaseURL)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize migration: %w", err)
	}

	m.Log = loggerWrapper{log.Default()}

	return m, nil
}
