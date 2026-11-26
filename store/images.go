package store

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/johansundell/scribble/images"
)

// ErrImageNotFound is returned for an image ID the store doesn't have.
var ErrImageNotFound = errors.New("image not found")

// ImageStore keeps the images notes show (#201, ADR 16). Only SQLStore
// implements it.
type ImageStore interface {
	// PutImage stores img, which images.Prepare made. An image that is
	// already stored is left as it is.
	PutImage(ctx context.Context, img images.Image) error
	// GetImage returns the image with id, or ErrImageNotFound.
	GetImage(ctx context.Context, id string) (images.Image, error)
	// ListImages returns every image, by ID, without its bytes.
	ListImages(ctx context.Context) ([]images.Image, error)
}

func (s *SQLStore) PutImage(ctx context.Context, img images.Image) error {
	_, err := s.db.ExecContext(ctx, `INSERT OR IGNORE INTO images (id, mime, data, created_at) VALUES (?, ?, ?, ?)`,
		img.ID, img.MIME, img.Data, timeArg(time.Now()))
	return err
}

func (s *SQLStore) GetImage(ctx context.Context, id string) (images.Image, error) {
	img := images.Image{ID: id}
	err := s.db.QueryRowContext(ctx, `SELECT mime, data FROM images WHERE id = ?`, id).Scan(&img.MIME, &img.Data)
	if errors.Is(err, sql.ErrNoRows) {
		return images.Image{}, ErrImageNotFound
	}
	if err != nil {
		return images.Image{}, err
	}
	return img, nil
}

func (s *SQLStore) ListImages(ctx context.Context) ([]images.Image, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, mime FROM images ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []images.Image
	for rows.Next() {
		var img images.Image
		if err := rows.Scan(&img.ID, &img.MIME); err != nil {
			return nil, err
		}
		out = append(out, img)
	}
	return out, rows.Err()
}
