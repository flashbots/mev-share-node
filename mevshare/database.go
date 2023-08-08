package mevshare

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"math/big"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/jmoiron/sqlx"
	_ "github.com/lib/pq"
)

var (
	ethToWei = big.NewInt(1e18)

	ErrBundleNotCancelled = errors.New("bundle not cancelled")
)

type DBSbundle struct {
	Hash               []byte         `db:"hash"`
	Signer             []byte         `db:"signer"`
	Cancelled          bool           `db:"cancelled"`
	AllowMatching      bool           `db:"allow_matching"`
	ReceivedAt         time.Time      `db:"received_at"`
	SimSuccess         bool           `db:"sim_success"`
	SimError           sql.NullString `db:"sim_error"`
	SimulatedAt        sql.NullTime   `db:"simulated_at"`
	SimEffGasPrice     sql.NullString `db:"sim_eff_gas_price"`
	SimProfit          sql.NullString `db:"sim_profit"`
	SimRefundableValue sql.NullString `db:"sim_refundable_value"`
	SimGasUsed         sql.NullInt64  `db:"sim_gas_used"`
	SimAllSimsGasUsed  sql.NullInt64  `db:"sim_all_sims_gas_used"`
	SimTotalSimCount   sql.NullInt64  `db:"sim_total_sim_count"`
	Body               []byte         `db:"body"`
	BodySize           int            `db:"body_size"`
	OriginID           sql.NullString `db:"origin_id"`
}

var insertBundleQuery = `
INSERT INTO sbundle (hash, signer, cancelled, allow_matching, received_at, 
                     sim_success, sim_error, simulated_at, sim_eff_gas_price, sim_profit, sim_refundable_value, sim_gas_used,
                     sim_all_sims_gas_used, sim_total_sim_count,
                     body, body_size, origin_id)
VALUES (:hash, :signer, :cancelled, :allow_matching, :received_at, 
        :sim_success, :sim_error, :simulated_at, :sim_eff_gas_price, :sim_profit, :sim_refundable_value, :sim_gas_used,
        :sim_all_sims_gas_used, :sim_total_sim_count,
        :body, :body_size, :origin_id)
ON CONFLICT (hash) DO NOTHING
RETURNING hash`

var selectSimDataBundleQueryForUpdate = `
SELECT sim_all_sims_gas_used, sim_total_sim_count
FROM sbundle
WHERE hash = $1
FOR UPDATE`

var updateBundleSimQuery = `
UPDATE sbundle
SET sim_success = :sim_success, sim_error = :sim_error, simulated_at = :simulated_at, 
    sim_eff_gas_price = :sim_eff_gas_price, sim_profit = :sim_profit, sim_refundable_value = :sim_refundable_value, 
    sim_gas_used = :sim_gas_used, sim_all_sims_gas_used = :sim_all_sims_gas_used, sim_total_sim_count = :sim_total_sim_count
WHERE hash = :hash`

var getBundleQuery = `
SELECT hash, body
FROM sbundle
WHERE hash = $1 AND allow_matching = true AND cancelled = false limit 1`

var cancelBundleQuery = `UPDATE sbundle SET cancelled = true WHERE hash = $1 AND signer = $2 AND cancelled = false RETURNING hash`

type DBSbundleBody struct {
	Hash        []byte `db:"hash"`
	ElementHash []byte `db:"element_hash"`
	Idx         int    `db:"idx"`
	Type        int    `db:"type"`
}

var insertBundleBodyQuery = `
INSERT INTO sbundle_body (hash, element_hash, idx, type)
VALUES (:hash, :element_hash, :idx, :type)
ON CONFLICT (hash, idx) DO NOTHING`

type DBSbundleBuilder struct {
	Hash           []byte         `db:"hash"`
	Block          int64          `db:"block"`
	MaxBlock       int64          `db:"max_block"`
	SimStateBlock  sql.NullInt64  `db:"sim_state_block"`
	SimEffGasPrice sql.NullString `db:"sim_eff_gas_price"`
	SimProfit      sql.NullString `db:"sim_profit"`
	Body           []byte         `db:"body"`
}

var insertBundleBuilderQuery = `
INSERT INTO sbundle_builder (hash, block, max_block, sim_state_block, sim_eff_gas_price, sim_profit, body)
VALUES (:hash, :block, :max_block, :sim_state_block, :sim_eff_gas_price, :sim_profit, :body)
ON conflict (hash) DO 
UPDATE SET block = :block, max_block = :max_block, sim_state_block = :sim_state_block, sim_eff_gas_price = :sim_eff_gas_price, sim_profit = :sim_profit, body = :body`

var ErrBundleNotFound = errors.New("bundle not found")

type DBSbundleHistoricalHint struct {
	ID         int64           `db:"id"`
	Block      int64           `db:"block"`
	Hint       json.RawMessage `db:"hint"`
	InsertedAt time.Time       `db:"inserted_at"`
}

var insertBundleHistoricalHintQuery = `
INSERT INTO sbundle_hint_history (block, hint)
VALUES (:block, :hint)
RETURNING id`

type DBBackend struct {
	db *sqlx.DB

	insertBundle        *sqlx.NamedStmt
	getBundle           *sqlx.Stmt
	insertBuilderBundle *sqlx.NamedStmt
	cancelBundle        *sqlx.Stmt
	insertHint          *sqlx.NamedStmt
	updateBundleSim     *sqlx.NamedStmt
}

func NewDBBackend(postgresDSN string) (*DBBackend, error) {
	db, err := sqlx.Connect("postgres", postgresDSN)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(10)
	db.SetMaxIdleConns(20)

	insertBundle, err := db.PrepareNamed(insertBundleQuery)
	if err != nil {
		return nil, err
	}
	getBundle, err := db.Preparex(getBundleQuery)
	if err != nil {
		return nil, err
	}
	insertBuilderBundle, err := db.PrepareNamed(insertBundleBuilderQuery)
	if err != nil {
		return nil, err
	}
	cancelBundle, err := db.Preparex(cancelBundleQuery)
	if err != nil {
		return nil, err
	}
	insertHint, err := db.PrepareNamed(insertBundleHistoricalHintQuery)
	if err != nil {
		return nil, err
	}

	updateBundleSim, err := db.PrepareNamed(updateBundleSimQuery)
	if err != nil {
		return nil, err
	}

	return &DBBackend{
		db:                  db,
		insertBundle:        insertBundle,
		getBundle:           getBundle,
		insertBuilderBundle: insertBuilderBundle,
		cancelBundle:        cancelBundle,
		insertHint:          insertHint,
		updateBundleSim:     updateBundleSim,
	}, nil
}

func (b *DBBackend) GetBundle(ctx context.Context, hash common.Hash) (*SendMevBundleArgs, error) {
	var dbSbundle DBSbundle
	err := b.getBundle.GetContext(ctx, &dbSbundle, hash.Bytes())
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrBundleNotFound
	} else if err != nil {
		return nil, err
	}

	var bundle SendMevBundleArgs
	err = json.Unmarshal(dbSbundle.Body, &bundle)
	if err != nil {
		return nil, err
	}
	return &bundle, nil
}

func (b *DBBackend) InsertBundleForStats(ctx context.Context, bundle *SendMevBundleArgs, result *SimMevBundleResponse) (known bool, err error) {
	var dbBundle DBSbundle
	if bundle.Metadata == nil {
		return known, ErrNilBundleMetadata
	}
	dbBundle.Hash = bundle.Metadata.BundleHash.Bytes()
	dbBundle.Signer = bundle.Metadata.Signer.Bytes()
	dbBundle.AllowMatching = bundle.Privacy != nil && bundle.Privacy.Hints.HasHint(HintHash)
	dbBundle.Cancelled = false
	dbBundle.ReceivedAt = time.UnixMicro(int64(bundle.Metadata.ReceivedAt))
	dbBundle.SimSuccess = result.Success
	dbBundle.SimError = sql.NullString{String: result.Error, Valid: result.Error != ""}
	dbBundle.SimulatedAt = sql.NullTime{Time: time.Now(), Valid: true}
	dbBundle.SimEffGasPrice = sql.NullString{String: dbIntToEth(&result.MevGasPrice), Valid: result.Success}
	dbBundle.SimProfit = sql.NullString{String: dbIntToEth(&result.Profit), Valid: result.Success}
	dbBundle.SimRefundableValue = sql.NullString{String: dbIntToEth(&result.RefundableValue), Valid: result.Success}
	dbBundle.SimGasUsed = sql.NullInt64{Int64: int64(result.GasUsed), Valid: true}
	dbBundle.SimAllSimsGasUsed = sql.NullInt64{Int64: int64(result.GasUsed), Valid: true}
	dbBundle.SimTotalSimCount = sql.NullInt64{Int64: 1, Valid: true}
	dbBundle.Body, err = json.Marshal(bundle)
	if err != nil {
		return known, err
	}
	dbBundle.BodySize = len(bundle.Body)
	dbBundle.OriginID = sql.NullString{String: bundle.Metadata.OriginID, Valid: bundle.Metadata.OriginID != ""}

	dbTx, err := b.db.BeginTxx(ctx, nil)
	if err != nil {
		return known, err
	}
	// get hash from db
	var hash []byte
	err = dbTx.NamedStmtContext(ctx, b.insertBundle).GetContext(ctx, &hash, dbBundle)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			// bundle is known so we update it with fresh simulation results
			known = true
			// 1. get bundle from db
			var storedBundle DBSbundle
			err = dbTx.GetContext(ctx, &storedBundle, selectSimDataBundleQueryForUpdate, dbBundle.Hash)

			if err != nil {
				_ = dbTx.Rollback()
				return known, err
			}
			storedBundle.SimSuccess = result.Success
			storedBundle.SimError = sql.NullString{String: result.Error, Valid: result.Error != ""}
			storedBundle.SimulatedAt = sql.NullTime{Time: time.Now(), Valid: true}
			storedBundle.SimEffGasPrice = sql.NullString{String: dbIntToEth(&result.MevGasPrice), Valid: result.Success}
			storedBundle.SimProfit = sql.NullString{String: dbIntToEth(&result.Profit), Valid: result.Success}
			storedBundle.SimRefundableValue = sql.NullString{String: dbIntToEth(&result.RefundableValue), Valid: result.Success}
			storedBundle.SimGasUsed = sql.NullInt64{Int64: int64(result.GasUsed), Valid: true}
			if storedBundle.SimTotalSimCount.Valid {
				storedBundle.SimAllSimsGasUsed = sql.NullInt64{Int64: storedBundle.SimAllSimsGasUsed.Int64 + int64(result.GasUsed), Valid: true}
			} else {
				storedBundle.SimAllSimsGasUsed = sql.NullInt64{Int64: int64(result.GasUsed), Valid: true}
			}
			if storedBundle.SimTotalSimCount.Valid {
				storedBundle.SimTotalSimCount = sql.NullInt64{Int64: storedBundle.SimTotalSimCount.Int64 + 1, Valid: true}
			} else {
				storedBundle.SimTotalSimCount = sql.NullInt64{Int64: 1, Valid: true}
			}
			// 2. update bundle
			_, err = dbTx.NamedExecContext(ctx, updateBundleSimQuery, storedBundle)
			if err != nil {
				_ = dbTx.Rollback()
				return known, err
			}

			_ = dbTx.Commit()
			return known, nil
		}
		_ = dbTx.Rollback()
		return known, err
	}

	// insert body
	bodyElements := make([]DBSbundleBody, len(bundle.Metadata.BodyHashes))
	for i, hash := range bundle.Metadata.BodyHashes {
		var bodyType int
		if i < len(bundle.Body) {
			if bundle.Body[i].Tx != nil {
				bodyType = 1
			} else if bundle.Body[i].Bundle != nil {
				bodyType = 2
			}
		}
		bodyElements[i] = DBSbundleBody{Hash: bundle.Metadata.BundleHash.Bytes(), ElementHash: hash.Bytes(), Idx: i, Type: bodyType}
	}

	_, err = dbTx.NamedExecContext(ctx, insertBundleBodyQuery, bodyElements)
	if err != nil {
		_ = dbTx.Rollback()
		return known, err
	}
	return known, dbTx.Commit()
}

func (b *DBBackend) CancelBundleByHash(ctx context.Context, hash common.Hash, signer common.Address) error {
	var result []byte
	err := b.cancelBundle.GetContext(ctx, &result, hash.Bytes(), signer.Bytes())
	if err != nil {
		return err
	}

	if !bytes.Equal(result, hash.Bytes()) {
		return ErrBundleNotCancelled
	}
	return nil
}

func (b *DBBackend) InsertBundleForBuilder(ctx context.Context, bundle *SendMevBundleArgs, result *SimMevBundleResponse, targetBlock uint64) error {
	var dbBundle DBSbundleBuilder
	var err error
	if bundle.Metadata == nil {
		return ErrNilBundleMetadata
	}
	dbBundle.Hash = bundle.Metadata.BundleHash.Bytes()
	dbBundle.Block = int64(targetBlock)
	dbBundle.MaxBlock = int64(targetBlock)
	dbBundle.SimStateBlock = sql.NullInt64{Int64: int64(result.StateBlock), Valid: result.Success}
	dbBundle.SimEffGasPrice = sql.NullString{String: dbIntToEth(&result.MevGasPrice), Valid: result.Success}
	dbBundle.SimProfit = sql.NullString{String: dbIntToEth(&result.Profit), Valid: result.Success}
	dbBundle.Body, err = json.Marshal(bundle)
	if err != nil {
		return err
	}

	_, err = b.insertBuilderBundle.ExecContext(ctx, dbBundle)
	return err
}

func dbIntToEth(i *hexutil.Big) string {
	return new(big.Rat).SetFrac(i.ToInt(), ethToWei).FloatString(18)
}

func (b *DBBackend) InsertHistoricalHint(ctx context.Context, currentBlock uint64, hint *Hint) error {
	var dbHint DBSbundleHistoricalHint

	dbHint.Block = int64(currentBlock)

	byteHint, err := json.Marshal(hint)
	if err != nil {
		return err
	}
	dbHint.Hint = byteHint

	_, err = b.insertHint.ExecContext(ctx, dbHint)
	return err
}
