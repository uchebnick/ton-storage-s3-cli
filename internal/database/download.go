package database

import "context"


func (db *DB) StartDownloadJob(ctx context.Context, fileID int64) (int64, error) {
	var jobID int64
	err := db.pool.QueryRow(ctx, `
		INSERT INTO downloads (file_id, status)
		VALUES ($1, 'running')
		RETURNING id
	`, fileID).Scan(&jobID)
	
	if err != nil {
		return 0, err
	}
	return jobID, nil
}

func (db *DB) FinishDownloadJob(ctx context.Context, jobID int64, success bool, errorMsg string) error {
	status := "completed"
	if !success {
		status = "failed"
	}

	tx, err := db.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	_, err = tx.Exec(ctx, `
		UPDATE downloads 
		SET status = $1, finished_at = NOW(), error_msg = $2 
		WHERE id = $3
	`, status, errorMsg, jobID)
	if err != nil {
		return err
	}

	if success {
		_, err = tx.Exec(ctx, `
			UPDATE files 
			SET status = 'active' 
			WHERE id = (SELECT file_id FROM downloads WHERE id = $1)
		`, jobID)
		if err != nil {
			return err
		}
	}

	return tx.Commit(ctx)
}

func (db *DB) IsFileDownloading(ctx context.Context, fileID int64) (bool, error) {
	var exists bool
	err := db.pool.QueryRow(ctx, `
		SELECT EXISTS(
			SELECT 1 FROM downloads 
			WHERE file_id = $1 AND status = 'running'
		)
	`, fileID).Scan(&exists)
	return exists, err
}

func (db *DB) ResetStuckDownloads(ctx context.Context) error {
	_, err := db.pool.Exec(ctx, `
		UPDATE downloads 
		SET status = 'failed', finished_at = NOW(), error_msg = 'Server restarted/crashed'
		WHERE status = 'running'
	`)
	return err
}