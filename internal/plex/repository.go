package plex

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"

	_ "modernc.org/sqlite"
)

type Repository struct {
	database *sql.DB
}

func Open(path string) (*Repository, error) {
	databaseURL := &url.URL{Scheme: "file", Path: path}
	query := databaseURL.Query()
	query.Set("mode", "ro")
	query.Add("_pragma", "busy_timeout(5000)")
	query.Add("_pragma", "query_only(1)")
	databaseURL.RawQuery = query.Encode()

	database, err := sql.Open("sqlite", databaseURL.String())
	if err != nil {
		return nil, fmt.Errorf("open Plex database: %w", err)
	}
	if err := database.Ping(); err != nil {
		_ = database.Close()
		return nil, fmt.Errorf("ping Plex database: %w", err)
	}
	return &Repository{database: database}, nil
}

func (repository *Repository) Close() error {
	return repository.database.Close()
}

func (repository *Repository) CountMediaUnder(ctx context.Context, directory string) (int, error) {
	const query = `
        SELECT count(*)
        FROM media_parts
        WHERE file = ? OR substr(file, 1, length(?) + 1) = ? || '/'
    `
	var count int
	if err := repository.database.QueryRowContext(ctx, query, directory, directory, directory).Scan(&count); err != nil {
		return 0, fmt.Errorf("count Plex media under %q: %w", directory, err)
	}
	return count, nil
}
