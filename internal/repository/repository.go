package repository

import (
	"database/sql"
	"errors"
	"time"
)

type Link struct {
	ID        int64
	FileKey   string
	URL       string
	CreatedAt time.Time
}

type Repository struct {
	db *sql.DB
}

func NewRepository(db *sql.DB) *Repository {
	return &Repository{db: db}
}

func (r *Repository) Save(linkRecord *Link) error {
	if r == nil {
		return errors.New("link repository is nil")
	}
	if linkRecord == nil {
		return errors.New("link record is nil")
	}
	if linkRecord.CreatedAt.IsZero() {
		linkRecord.CreatedAt = time.Now().UTC()
	}
	_, err := r.db.Exec(
		"INSERT INTO links (file_key, url, created_at) VALUES (?, ?, ?)",
		linkRecord.FileKey,
		linkRecord.URL,
		linkRecord.CreatedAt,
	)
	return err
}
