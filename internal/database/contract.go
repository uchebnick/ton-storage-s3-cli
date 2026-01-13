package database

import "context"

func (db *DB) RegisterContract(ctx context.Context, c *Contract) error {
	_, err := db.pool.Exec(ctx, `
		INSERT INTO contracts (file_id, provider_addr, contract_addr, balance_nano_ton, status)
		VALUES ($1, $2, $3, $4, 'pending')
	`, c.FileID, c.ProviderAddr, c.ContractAddr, c.BalanceNano)
	return err
}

func (db *DB) MarkContractFailed(ctx context.Context, contractID int64) error {
	_, err := db.pool.Exec(ctx, `UPDATE contracts SET status = 'failed', last_check = NOW() WHERE id = $1`, contractID)
	return err
}

func (db *DB) MarkContractActive(ctx context.Context, contractID int64) error {
	_, err := db.pool.Exec(ctx, `UPDATE contracts SET status = 'active', last_check = NOW() WHERE id = $1`, contractID)
	return err
}

func (db *DB) UpdateContractCheck(ctx context.Context, contractID int64) error {
	_, err := db.pool.Exec(ctx, `UPDATE contracts SET last_check = NOW() WHERE id = $1`, contractID)
	return err
}

func (db *DB) GetAllContracts(ctx context.Context, totalWorkers, workerID int) ([]ContractWithMeta, error) {
	query := `
		SELECT c.id, c.file_id, c.provider_addr, c.contract_addr, c.balance_nano_ton, c.last_check, f.bag_id
		FROM contracts c
		JOIN files f ON c.file_id = f.id
		WHERE c.id % $1 = $2
		ORDER BY c.last_check ASC 
		LIMIT 20
	`
	rows, err := db.pool.Query(ctx, query, totalWorkers, workerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []ContractWithMeta
	for rows.Next() {
		var c ContractWithMeta
		if err := rows.Scan(&c.ID, &c.FileID, &c.ProviderAddr, &c.ContractAddr, &c.BalanceNano, &c.LastCheck, &c.BagID); err != nil {
			return nil, err
		}
		result = append(result, c)
	}
	return result, nil
}

func (db *DB) GetActiveContracts(ctx context.Context, totalWorkers, workerID int) ([]ContractWithMeta, error) {
	query := `
		SELECT c.id, c.file_id, c.provider_addr, c.contract_addr, c.balance_nano_ton, c.last_check, f.bag_id
		FROM contracts c
		JOIN files f ON c.file_id = f.id
		WHERE c.status = 'active' 
		  AND c.created_at < NOW() - INTERVAL '12 hours'
		  AND c.id % $1 = $2
		ORDER BY c.last_check ASC 
		LIMIT 20
	`
	rows, err := db.pool.Query(ctx, query, totalWorkers, workerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []ContractWithMeta
	for rows.Next() {
		var c ContractWithMeta
		if err := rows.Scan(&c.ID, &c.FileID, &c.ProviderAddr, &c.ContractAddr, &c.BalanceNano, &c.LastCheck, &c.BagID); err != nil {
			return nil, err
		}
		result = append(result, c)
	}
	return result, nil
}

func (db *DB) GetContractByBagID(ctx context.Context, bagID string) (*ContractWithMeta, error) {
	query := `
		SELECT c.id, c.file_id, c.provider_addr, c.contract_addr, c.balance_nano_ton, c.last_check, f.bag_id
		FROM contracts c
		JOIN files f ON c.file_id = f.id
		WHERE f.bag_id = $1
		ORDER BY c.id DESC
		LIMIT 1
	`
	var c ContractWithMeta
	err := db.pool.QueryRow(ctx, query, bagID).Scan(
		&c.ID, &c.FileID, &c.ProviderAddr, &c.ContractAddr, &c.BalanceNano, &c.LastCheck, &c.BagID,
	)
	if err != nil {
		return nil, err
	}
	return &c, nil
}

func (db *DB) GetContractByID(ctx context.Context, id int64) (*ContractWithMeta, error) {
	query := `
		SELECT c.id, c.file_id, c.provider_addr, c.contract_addr, c.balance_nano_ton, c.last_check, f.bag_id
		FROM contracts c
		JOIN files f ON c.file_id = f.id
		WHERE c.id = $1
	`
	var c ContractWithMeta
	err := db.pool.QueryRow(ctx, query, id).Scan(
		&c.ID, &c.FileID, &c.ProviderAddr, &c.ContractAddr, &c.BalanceNano, &c.LastCheck, &c.BagID,
	)
	if err != nil {
		return nil, err
	}
	return &c, nil
}

func (db *DB) GetFileContracts(ctx context.Context, fileID int64) ([]Contract, error) {
	rows, err := db.pool.Query(ctx, `
		SELECT id, file_id, provider_addr, contract_addr, balance_nano_ton, status, last_check
		FROM contracts WHERE file_id=$1
	`, fileID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []Contract
	for rows.Next() {
		var c Contract
		if err := rows.Scan(&c.ID, &c.FileID, &c.ProviderAddr, &c.ContractAddr, &c.BalanceNano, &c.Status, &c.LastCheck); err != nil {
			return nil, err
		}
		result = append(result, c)
	}
	return result, nil
}

func (db *DB) GetContractsForAudit(ctx context.Context, totalWorkers, workerID int) ([]ContractWithMeta, error) {
	query := `
		SELECT c.id, c.file_id, c.provider_addr, c.contract_addr, c.balance_nano_ton, c.last_check, f.bag_id, c.status
		FROM contracts c
		JOIN files f ON c.file_id = f.id
		WHERE c.status IN ('active', 'pending') -- Берем и активные, и ждущие
		  AND c.id % $1 = $2
		  AND c.last_check < NOW() - INTERVAL '10 minutes'
		ORDER BY c.last_check ASC 
		LIMIT 20
	`
	rows, err := db.pool.Query(ctx, query, totalWorkers, workerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []ContractWithMeta
	for rows.Next() {
		var c ContractWithMeta
		if err := rows.Scan(&c.ID, &c.FileID, &c.ProviderAddr, &c.ContractAddr, &c.BalanceNano, &c.LastCheck, &c.BagID, &c.Status); err != nil {
			return nil, err
		}
		result = append(result, c)
	}
	return result, nil
}