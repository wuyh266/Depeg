package mysql

import (
	"context"
	"depeg.local/platform/migrations"
	"fmt"
	"regexp"
	"sort"
	"strings"
)

func (c *client) Migrate(ctx context.Context) error {
	db, err := c.Session(ctx)
	if err != nil {
		return err
	}
	if err := db.Exec("CREATE TABLE IF NOT EXISTS schema_migrations (version VARCHAR(128) PRIMARY KEY, applied_at DATETIME(3) NOT NULL)").Error; err != nil {
		return translate(err)
	}
	entries, err := migrations.Files.ReadDir(".")
	if err != nil {
		return err
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		if strings.HasSuffix(entry.Name(), ".up.sql") {
			names = append(names, entry.Name())
		}
	}
	sort.Strings(names)
	for _, name := range names {
		var count int64
		if err := db.Table("schema_migrations").Where("version = ?", name).Count(&count).Error; err != nil {
			return translate(err)
		}
		if count > 0 {
			continue
		}
		content, err := migrations.Files.ReadFile(name)
		if err != nil {
			return err
		}
		// The checked-in migrations deliberately contain only simple DDL statements;
		// semicolons inside literals or stored procedures are not supported by this runner.
		lines := []string{}
		for _, line := range strings.Split(string(content), "\n") {
			if !strings.HasPrefix(strings.TrimSpace(line), "--") {
				lines = append(lines, line)
			}
		}
		for _, statement := range strings.Split(strings.Join(lines, "\n"), ";") {
			statement = strings.TrimSpace(statement)
			if statement == "" {
				continue
			}
			// MySQL DDL commits implicitly. Resume a partially applied additive
			// migration without trying to add an already existing column.
			if match := addColumn.FindStringSubmatch(statement); match != nil {
				var existing int64
				if err := db.Raw("SELECT COUNT(*) FROM information_schema.columns WHERE table_schema = DATABASE() AND table_name = ? AND column_name = ?", match[1], match[2]).Scan(&existing).Error; err != nil {
					return translate(err)
				}
				if existing > 0 {
					continue
				}
			}
			if err := db.Exec(statement).Error; err != nil {
				return fmt.Errorf("migration %s: %w", name, translate(err))
			}
		}
		if err := db.Exec("INSERT INTO schema_migrations (version, applied_at) VALUES (?, UTC_TIMESTAMP(3))", name).Error; err != nil {
			return translate(err)
		}
	}
	return nil
}

var addColumn = regexp.MustCompile(`(?i)^ALTER\s+TABLE\s+([a-z_][a-z0-9_]*)\s+ADD\s+COLUMN\s+([a-z_][a-z0-9_]*)\s+`)
