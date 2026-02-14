package repository

import (
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"
)

type FileRecord struct {
	FileID   string
	FileKey  string
	Caption  string
	FileType string
}

type FileRepository struct {
	db *sql.DB
}

func NewFileRepository(db *sql.DB) *FileRepository {
	return &FileRepository{db: db}
}

func (r *FileRepository) Save(fileID, fileType, caption string) (string, error) {
	fileKey, err := generateKey()
	if err != nil {
		return "", err
	}
	_, err = r.db.Exec(
		"INSERT INTO files (file_id, file_key, caption, file_type) VALUES ($1, $2, $3, $4)",
		fileID, fileKey, caption, fileType,
	)
	if err != nil {
		return "", fmt.Errorf("save file: %w", err)
	}
	return fileKey, nil
}

func (r *FileRepository) UpdateCaption(fileKey, caption string) error {
	_, err := r.db.Exec(
		"UPDATE files SET caption = $1 WHERE file_key = $2",
		caption, fileKey,
	)
	return err
}

func (r *FileRepository) Get(fileKey string) (*FileRecord, error) {
	row := r.db.QueryRow(
		"SELECT file_id, caption, file_type FROM files WHERE file_key = $1",
		fileKey,
	)
	record := &FileRecord{}
	if err := row.Scan(&record.FileID, &record.Caption, &record.FileType); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return record, nil
}

func generateKey() (string, error) {
	buf := make([]byte, 8)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}
