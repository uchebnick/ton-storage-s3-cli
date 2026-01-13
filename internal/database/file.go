package database

import (
	"context"
	"time"
)


func (db *DB) CreateFile(ctx context.Context, f *File) (int64, error) {
	var id int64
	err := db.pool.QueryRow(ctx, `
		INSERT INTO files (bucket_name, object_key, bag_id, size_bytes, target_replicas, status)
		VALUES ($1, $2, $3, $4, $5, 'pending')
		RETURNING id
	`, f.BucketName, f.ObjectKey, f.BagID, f.SizeBytes, f.TargetReplicas).Scan(&id)
	return id, err
}

func (db *DB) GetFilesNeedingReplication(ctx context.Context, totalWorkers, workerID int) ([]FileWithStatus, error) {
	query := `
		SELECT 
			f.id, f.bucket_name, f.object_key, f.bag_id, f.target_replicas, 
			COUNT(c.id) as active_count,
			COALESCE(array_agg(c.provider_addr) FILTER (WHERE c.provider_addr IS NOT NULL), '{}') as used_providers
		FROM files f
		LEFT JOIN contracts c ON f.id = c.file_id AND (c.status = 'active' OR c.status = 'pending')
		WHERE f.status != 'deleted' 
		  AND f.id % $1 = $2
		GROUP BY f.id
		HAVING COUNT(c.id) < f.target_replicas
		LIMIT 50
	`

	rows, err := db.pool.Query(ctx, query, totalWorkers, workerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []FileWithStatus
	for rows.Next() {
		var item FileWithStatus
		if err := rows.Scan(
			&item.ID, &item.BucketName, &item.ObjectKey, &item.BagID, &item.TargetReplicas,
			&item.ActiveReplicas, &item.UsedProviders,
		); err != nil {
			return nil, err
		}
		result = append(result, item)
	}
	return result, nil
}

func (db *DB) ListFiles(ctx context.Context, limit, offset int) ([]File, error) {
	rows, err := db.pool.Query(ctx, `
		SELECT id, bucket_name, object_key, bag_id, size_bytes, target_replicas, status, created_at 
		FROM files 
		ORDER BY created_at DESC 
		LIMIT $1 OFFSET $2
	`, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []File
	for rows.Next() {
		var f File

		if err := rows.Scan(&f.ID, &f.BucketName, &f.ObjectKey, &f.BagID, &f.SizeBytes, &f.TargetReplicas, &f.Status, &f.CreatedAt); err != nil {
			return nil, err
		}
		result = append(result, f)
	}
	return result, nil
}

func (db *DB) GetFileByID(ctx context.Context, id int64) (*File, error) {
	f := &File{}
	err := db.pool.QueryRow(ctx, `
		SELECT id, bucket_name, object_key, bag_id, size_bytes, target_replicas, status, created_at
		FROM files WHERE id=$1
	`, id).Scan(&f.ID, &f.BucketName, &f.ObjectKey, &f.BagID, &f.SizeBytes, &f.TargetReplicas, &f.Status, &f.CreatedAt)
	if err != nil {
		return nil, err
	}
	return f, nil
}

func (db *DB) GetFilesReadyForCleaning(ctx context.Context, interval time.Duration, totalWorkers, workerID, limit int) ([]File, error) {
	query := `
		SELECT f.id, f.bucket_name, f.object_key, f.bag_id, f.size_bytes, f.target_replicas, f.status, f.created_at
		FROM files f
		LEFT JOIN downloads d ON f.id = d.file_id AND d.status = 'running'
		WHERE f.created_at < (NOW() - $1::interval)
		  AND f.id % $2 = $3
		  AND f.status = 'active'
		  AND d.id IS NULL
		ORDER BY f.created_at ASC
		LIMIT $4
	`

	rows, err := db.pool.Query(ctx, query, interval, totalWorkers, workerID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []File
	for rows.Next() {
		var f File
		if err := rows.Scan(
			&f.ID, &f.BucketName, &f.ObjectKey, &f.BagID, &f.SizeBytes, 
			&f.TargetReplicas, &f.Status, &f.CreatedAt,
		); err != nil {
			return nil, err
		}
		result = append(result, f)
	}
	return result, nil
}

func (db *DB) DeleteFile(ctx context.Context, bucketName, objectKey string) error {
	_, err := db.pool.Exec(ctx, `
		DELETE FROM files 
		WHERE bucket_name = $1 AND object_key = $2
	`, bucketName, objectKey)
	return err
}

func (db *DB) GetFileMeta(ctx context.Context, bucketName, objectKey string) (*File, error) {
	f := &File{}
	err := db.pool.QueryRow(ctx, `
		SELECT id, bucket_name, object_key, bag_id, size_bytes, target_replicas, status, created_at
		FROM files 
		WHERE bucket_name = $1 AND object_key = $2
	`, bucketName, objectKey).Scan(
		&f.ID, &f.BucketName, &f.ObjectKey, &f.BagID, &f.SizeBytes, 
		&f.TargetReplicas, &f.Status, &f.CreatedAt,
	)
	if err != nil {
		return nil, err
	}
	return f, nil
}

func (db *DB) UpgradeFileStatusIfNeeded(ctx context.Context, fileID int64) error {
	_, err := db.pool.Exec(ctx, `
		UPDATE files f
		SET status = 'active'
		WHERE f.id = $1
		  AND f.status = 'pending'
		  AND (
			SELECT COUNT(*) 
			FROM contracts c 
			WHERE c.file_id = f.id AND c.status = 'active'
		  ) >= f.target_replicas
	`, fileID)
	return err
}

func (db *DB) DowngradeFileStatusIfNeeded(ctx context.Context, fileID int64) error {
	_, err := db.pool.Exec(ctx, `
		UPDATE files f
		SET status = 'pending'
		WHERE f.id = $1
		  AND f.status = 'active'
		  AND (
			SELECT COUNT(*) 
			FROM contracts c 
			WHERE c.file_id = f.id AND c.status = 'active'
		  ) < f.target_replicas
	`, fileID)
	return err
}