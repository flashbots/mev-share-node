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
	Body               []byte         `db:"body"`
	BodySize           int            `db:"body_size"`
	OriginID           sql.NullString `db:"origin_id"`
}

var insertBundleQuery = `
INSERT INTO sbundle (hash, signer, cancelled, allow_matching, received_at, 
                     sim_success, sim_error, simulated_at, sim_eff_gas_price, sim_profit, sim_refundable_value, sim_gas_used,
                     body, body_size, origin_id)
VALUES (:hash, :signer, :cancelled, :allow_matching, :received_at, 
        :sim_success, :sim_error, :simulated_at, :sim_eff_gas_price, :sim_profit, :sim_refundable_value, :sim_gas_used,
        :body, :body_size, :origin_id)
ON CONFLICT (hash) DO NOTHING
RETURNING hash`

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
ON conflict (hash) DO NOTHING`

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

	return &DBBackend{
		db:                  db,
		insertBundle:        insertBundle,
		getBundle:           getBundle,
		insertBuilderBundle: insertBuilderBundle,
		cancelBundle:        cancelBundle,
		insertHint:          insertHint,
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

func (b *DBBackend) InsertBundleForStats(ctx context.Context, bundle *SendMevBundleArgs, result *SimMevBundleResponse) error {
	var dbBundle DBSbundle
	var err error
	if bundle.Metadata == nil {
		return ErrNilBundleMetadata
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
	dbBundle.Body, err = json.Marshal(bundle)
	if err != nil {
		return err
	}
	dbBundle.BodySize = len(bundle.Body)
	dbBundle.OriginID = sql.NullString{String: bundle.Metadata.OriginID, Valid: bundle.Metadata.OriginID != ""}

	dbTx, err := b.db.BeginTxx(ctx, nil)
	if err != nil {
		return err
	}
	// get hash from db
	var hash []byte
	err = dbTx.NamedStmtContext(ctx, b.insertBundle).GetContext(ctx, &hash, dbBundle)
	if err != nil {
		_ = dbTx.Rollback()
		if errors.Is(err, sql.ErrNoRows) {
			return ErrKnownBundle
		}
		return err
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
		return err
	}
	return dbTx.Commit()
}

func (b *DBBackend) CancelBundleByHash(ctx context.Context, hash common.Hash, signer common.Address) error {
	// TODO: improve
	// this is very primitive bundle cancellation, we should also cancel all the bundles that are dependent on this one
	// but for now while we have 1 level of dependencies, this is enough because builder will cancel all the bundles that are dependent on this one
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

func (b *DBBackend) InsertBundleForBuilder(ctx context.Context, bundle *SendMevBundleArgs, result *SimMevBundleResponse) error {
	var dbBundle DBSbundleBuilder
	var err error
	if bundle.Metadata == nil {
		return ErrNilBundleMetadata
	}
	dbBundle.Hash = bundle.Metadata.BundleHash.Bytes()
	dbBundle.Block = int64(bundle.Inclusion.BlockNumber)
	dbBundle.MaxBlock = int64(bundle.Inclusion.MaxBlock)
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
