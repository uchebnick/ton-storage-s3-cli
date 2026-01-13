package database

import (
	"context"
	"time"
)

type Bucket struct {
	Name		string
	CreatedAt	time.Time
}

func (db *DB) CreateBucket(ctx context.Context, name string) error {
	_, err := db.pool.Exec(ctx, "INSERT INTO buckets (name) VALUES ($1) ON CONFLICT DO NOTHING", name)
	return err
}

func (db *DB) DeleteBucket(ctx context.Context, name string) error {
	_, err := db.pool.Exec(ctx, "DELETE FROM buckets WHERE name=$1", name)
	return err
}

func (db *DB) BucketExists(ctx context.Context, name string) (bool, error) {
	var exists bool
	err := db.pool.QueryRow(ctx, "SELECT exists(SELECT 1 FROM buckets WHERE name=$1)", name).Scan(&exists)
	return exists, err
}

func (db *DB) ListBuckets(ctx context.Context) ([]Bucket, error) {
	rows, err := db.pool.Query(ctx, "SELECT name, created_at FROM buckets")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []Bucket
	for rows.Next() {
		var b Bucket
		if err := rows.Scan(&b.Name, &b.CreatedAt); err != nil {
			return nil, err
		}
		result = append(result, b)
	}
	return result, nil
}



