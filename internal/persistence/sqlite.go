package persistence

import (
	"context"
	"database/sql"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"time"

	_ "github.com/mattn/go-sqlite3"
	"github.com/rs/zerolog/log"
)

// Store provides SQLite-based persistence for operational state.
type Store struct {
	db *sql.DB
}

// PoolRecord represents a pool stored in the database.
type PoolRecord struct {
	Address    string
	Token0     string
	Token1     string
	Reserve0   string
	Reserve1   string
	Fee        float64
	IsStable   bool
	TVL        float64
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

// TokenRecord represents a token stored in the database.
type TokenRecord struct {
	Address   string
	Symbol    string
	Decimals  int
	CreatedAt time.Time
}

// NewStore creates a new SQLite store and runs migrations.
func NewStore(dbPath string) (*Store, error) {
	// Ensure directory exists
	dir := filepath.Dir(dbPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("creating database directory: %w", err)
	}

	db, err := sql.Open("sqlite3", dbPath+"?_journal_mode=WAL&_synchronous=NORMAL&_busy_timeout=5000")
	if err != nil {
		return nil, fmt.Errorf("opening database: %w", err)
	}

	// Set connection pool settings
	db.SetMaxOpenConns(1) // SQLite only supports one writer
	db.SetMaxIdleConns(1)
	db.SetConnMaxLifetime(0)

	store := &Store{db: db}

	if err := store.migrate(); err != nil {
		db.Close()
		return nil, fmt.Errorf("running migrations: %w", err)
	}

	return store, nil
}

// migrate runs database schema migrations.
func (s *Store) migrate() error {
	migrations := []string{
		`CREATE TABLE IF NOT EXISTS tokens (
			address TEXT PRIMARY KEY,
			symbol TEXT NOT NULL,
			decimals INTEGER NOT NULL,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE TABLE IF NOT EXISTS pools (
			address TEXT PRIMARY KEY,
			token0 TEXT NOT NULL,
			token1 TEXT NOT NULL,
			reserve0 TEXT NOT NULL DEFAULT '0',
			reserve1 TEXT NOT NULL DEFAULT '0',
			fee REAL NOT NULL DEFAULT 0.003,
			is_stable INTEGER NOT NULL DEFAULT 0,
			tvl REAL NOT NULL DEFAULT 0,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			FOREIGN KEY (token0) REFERENCES tokens(address),
			FOREIGN KEY (token1) REFERENCES tokens(address)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_pools_tvl ON pools(tvl DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_pools_tokens ON pools(token0, token1)`,
		`CREATE TABLE IF NOT EXISTS system_state (
			key TEXT PRIMARY KEY,
			value TEXT NOT NULL,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE TABLE IF NOT EXISTS tracked_pools (
			pool_address TEXT PRIMARY KEY,
			added_at DATETIME DEFAULT CURRENT_TIMESTAMP
		)`,
	}

	for _, migration := range migrations {
		if _, err := s.db.Exec(migration); err != nil {
			return fmt.Errorf("executing migration: %w", err)
		}
	}

	log.Info().Msg("Database migrations completed")
	return nil
}

// Close closes the database connection.
func (s *Store) Close() error {
	return s.db.Close()
}

// UpsertToken inserts or updates a token record.
func (s *Store) UpsertToken(ctx context.Context, token TokenRecord) error {
	query := `INSERT INTO tokens (address, symbol, decimals, created_at)
		VALUES (?, ?, ?, ?)
		ON CONFLICT(address) DO UPDATE SET symbol = excluded.symbol, decimals = excluded.decimals`

	_, err := s.db.ExecContext(ctx, query, token.Address, token.Symbol, token.Decimals, time.Now())
	return err
}

// UpsertPool inserts or updates a pool record.
func (s *Store) UpsertPool(ctx context.Context, pool PoolRecord) error {
	query := `INSERT INTO pools (address, token0, token1, reserve0, reserve1, fee, is_stable, tvl, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(address) DO UPDATE SET
			reserve0 = excluded.reserve0,
			reserve1 = excluded.reserve1,
			fee = excluded.fee,
			tvl = excluded.tvl,
			updated_at = excluded.updated_at`

	now := time.Now()
	_, err := s.db.ExecContext(ctx, query,
		pool.Address, pool.Token0, pool.Token1,
		pool.Reserve0, pool.Reserve1,
		pool.Fee, pool.IsStable, pool.TVL,
		now, now,
	)
	return err
}

// BulkUpsertTokens inserts or updates multiple token records efficiently.
func (s *Store) BulkUpsertTokens(ctx context.Context, tokens []TokenRecord) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("beginning transaction: %w", err)
	}
	defer tx.Rollback()

	stmt, err := tx.PrepareContext(ctx, `INSERT INTO tokens (address, symbol, decimals, created_at)
		VALUES (?, ?, ?, ?)
		ON CONFLICT(address) DO UPDATE SET symbol = excluded.symbol, decimals = excluded.decimals`)
	if err != nil {
		return fmt.Errorf("preparing statement: %w", err)
	}
	defer stmt.Close()

	now := time.Now()
	for _, token := range tokens {
		if _, err := stmt.ExecContext(ctx, token.Address, token.Symbol, token.Decimals, now); err != nil {
			return fmt.Errorf("inserting token %s: %w", token.Address, err)
		}
	}

	return tx.Commit()
}

// BulkUpsertPools inserts or updates multiple pool records efficiently.
func (s *Store) BulkUpsertPools(ctx context.Context, pools []PoolRecord) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("beginning transaction: %w", err)
	}
	defer tx.Rollback()

	stmt, err := tx.PrepareContext(ctx, `INSERT INTO pools (address, token0, token1, reserve0, reserve1, fee, is_stable, tvl, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(address) DO UPDATE SET
			reserve0 = excluded.reserve0,
			reserve1 = excluded.reserve1,
			fee = excluded.fee,
			tvl = excluded.tvl,
			updated_at = excluded.updated_at`)
	if err != nil {
		return fmt.Errorf("preparing statement: %w", err)
	}
	defer stmt.Close()

	now := time.Now()
	for _, pool := range pools {
		if _, err := stmt.ExecContext(ctx, pool.Address, pool.Token0, pool.Token1,
			pool.Reserve0, pool.Reserve1, pool.Fee, pool.IsStable, pool.TVL,
			now, now); err != nil {
			return fmt.Errorf("inserting pool %s: %w", pool.Address, err)
		}
	}

	return tx.Commit()
}

// GetTopPoolsByTVL retrieves the top N pools ordered by TVL.
func (s *Store) GetTopPoolsByTVL(ctx context.Context, limit int) ([]PoolRecord, error) {
	query := `SELECT address, token0, token1, reserve0, reserve1, fee, is_stable, tvl, created_at, updated_at
		FROM pools
		WHERE is_stable = 0
		ORDER BY tvl DESC
		LIMIT ?`

	rows, err := s.db.QueryContext(ctx, query, limit)
	if err != nil {
		return nil, fmt.Errorf("querying pools: %w", err)
	}
	defer rows.Close()

	var pools []PoolRecord
	for rows.Next() {
		var p PoolRecord
		if err := rows.Scan(&p.Address, &p.Token0, &p.Token1, &p.Reserve0, &p.Reserve1,
			&p.Fee, &p.IsStable, &p.TVL, &p.CreatedAt, &p.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scanning row: %w", err)
		}
		pools = append(pools, p)
	}

	return pools, rows.Err()
}

// GetAllPools retrieves all non-stable pools.
func (s *Store) GetAllPools(ctx context.Context) ([]PoolRecord, error) {
	query := `SELECT address, token0, token1, reserve0, reserve1, fee, is_stable, tvl, created_at, updated_at
		FROM pools
		WHERE is_stable = 0`

	rows, err := s.db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("querying pools: %w", err)
	}
	defer rows.Close()

	var pools []PoolRecord
	for rows.Next() {
		var p PoolRecord
		if err := rows.Scan(&p.Address, &p.Token0, &p.Token1, &p.Reserve0, &p.Reserve1,
			&p.Fee, &p.IsStable, &p.TVL, &p.CreatedAt, &p.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scanning row: %w", err)
		}
		pools = append(pools, p)
	}

	return pools, rows.Err()
}

// GetToken retrieves a token by address.
func (s *Store) GetToken(ctx context.Context, address string) (*TokenRecord, error) {
	query := `SELECT address, symbol, decimals, created_at FROM tokens WHERE address = ?`

	var t TokenRecord
	err := s.db.QueryRowContext(ctx, query, address).Scan(&t.Address, &t.Symbol, &t.Decimals, &t.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &t, nil
}

// GetAllTokens retrieves all tokens.
func (s *Store) GetAllTokens(ctx context.Context) ([]TokenRecord, error) {
	query := `SELECT address, symbol, decimals, created_at FROM tokens`

	rows, err := s.db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("querying tokens: %w", err)
	}
	defer rows.Close()

	var tokens []TokenRecord
	for rows.Next() {
		var t TokenRecord
		if err := rows.Scan(&t.Address, &t.Symbol, &t.Decimals, &t.CreatedAt); err != nil {
			return nil, fmt.Errorf("scanning row: %w", err)
		}
		tokens = append(tokens, t)
	}

	return tokens, rows.Err()
}

// GetPoolCount returns the total number of pools.
func (s *Store) GetPoolCount(ctx context.Context) (int, error) {
	var count int
	err := s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM pools").Scan(&count)
	return count, err
}

// SetTrackedPools sets the list of pools being tracked for events.
func (s *Store) SetTrackedPools(ctx context.Context, addresses []string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("beginning transaction: %w", err)
	}
	defer tx.Rollback()

	// Clear existing tracked pools
	if _, err := tx.ExecContext(ctx, "DELETE FROM tracked_pools"); err != nil {
		return fmt.Errorf("clearing tracked pools: %w", err)
	}

	// Insert new tracked pools
	stmt, err := tx.PrepareContext(ctx, "INSERT INTO tracked_pools (pool_address) VALUES (?)")
	if err != nil {
		return fmt.Errorf("preparing statement: %w", err)
	}
	defer stmt.Close()

	for _, addr := range addresses {
		if _, err := stmt.ExecContext(ctx, addr); err != nil {
			return fmt.Errorf("inserting tracked pool: %w", err)
		}
	}

	return tx.Commit()
}

// GetTrackedPools retrieves the list of pools being tracked.
func (s *Store) GetTrackedPools(ctx context.Context) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT pool_address FROM tracked_pools")
	if err != nil {
		return nil, fmt.Errorf("querying tracked pools: %w", err)
	}
	defer rows.Close()

	var addresses []string
	for rows.Next() {
		var addr string
		if err := rows.Scan(&addr); err != nil {
			return nil, fmt.Errorf("scanning row: %w", err)
		}
		addresses = append(addresses, addr)
	}

	return addresses, rows.Err()
}

// SetSystemState stores a key-value pair in system state.
func (s *Store) SetSystemState(ctx context.Context, key, value string) error {
	query := `INSERT INTO system_state (key, value, updated_at)
		VALUES (?, ?, ?)
		ON CONFLICT(key) DO UPDATE SET value = excluded.value, updated_at = excluded.updated_at`

	_, err := s.db.ExecContext(ctx, query, key, value, time.Now())
	return err
}

// GetSystemState retrieves a value from system state.
func (s *Store) GetSystemState(ctx context.Context, key string) (string, error) {
	var value string
	err := s.db.QueryRowContext(ctx, "SELECT value FROM system_state WHERE key = ?", key).Scan(&value)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return value, err
}

// UpdatePoolReserves updates the reserves for a pool.
func (s *Store) UpdatePoolReserves(ctx context.Context, address string, reserve0, reserve1 *big.Int) error {
	query := `UPDATE pools SET reserve0 = ?, reserve1 = ?, updated_at = ? WHERE address = ?`
	_, err := s.db.ExecContext(ctx, query, reserve0.String(), reserve1.String(), time.Now(), address)
	return err
}

// GetPoolByAddress retrieves a pool by its address.
func (s *Store) GetPoolByAddress(ctx context.Context, address string) (*PoolRecord, error) {
	query := `SELECT address, token0, token1, reserve0, reserve1, fee, is_stable, tvl, created_at, updated_at
		FROM pools WHERE address = ?`

	var p PoolRecord
	err := s.db.QueryRowContext(ctx, query, address).Scan(
		&p.Address, &p.Token0, &p.Token1, &p.Reserve0, &p.Reserve1,
		&p.Fee, &p.IsStable, &p.TVL, &p.CreatedAt, &p.UpdatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &p, nil
}
