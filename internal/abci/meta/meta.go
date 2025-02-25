// Package meta defines a chain metadata store for the ABCI application. Prior
// to using the methods, the tables should be initialized and updated to the
// latest schema version with InitializeMetaStore.
package meta

import (
	"context"
	"encoding/binary"
	"fmt"
	"slices"

	"github.com/kwilteam/kwil-db/common"
	"github.com/kwilteam/kwil-db/common/sql"
	"github.com/kwilteam/kwil-db/internal/sql/versioning"
)

const (
	chainSchemaName = `kwild_chain`

	chainStoreVersion = 1

	initChainTable = `CREATE TABLE IF NOT EXISTS ` + chainSchemaName + `.chain (
		height INT8 NOT NULL,
		app_hash BYTEA
	);` // no primary key, only one row
	initConsensusParamsTable = `CREATE TABLE IF NOT EXISTS ` + chainSchemaName + `.consensus_params (
		param_name TEXT PRIMARY KEY,
		param_value BYTEA
	)`

	insertChainState = `INSERT INTO ` + chainSchemaName + `.chain ` +
		`VALUES ($1, $2);`

	setChainState = `UPDATE ` + chainSchemaName + `.chain ` +
		`SET height = $1, app_hash = $2;`

	getChainState = `SELECT height, app_hash FROM ` + chainSchemaName + `.chain;`

	upsertParam = `INSERT INTO ` + chainSchemaName + `.consensus_params ` +
		`VALUES ($1, $2) ` +
		`ON CONFLICT (param_name) DO UPDATE SET param_value = $2;`

	getParams = `SELECT param_name, param_value FROM ` + chainSchemaName + `.consensus_params;`
)

func initTables(ctx context.Context, tx sql.DB) error {
	_, err := tx.Execute(ctx, initChainTable)
	return err
}

// InitializeMetaStore initializes the chain metadata store schema.
func InitializeMetaStore(ctx context.Context, db sql.DB) error {
	upgradeFns := map[int64]versioning.UpgradeFunc{
		0: initTables,
		1: func(ctx context.Context, db sql.DB) error {
			_, err := db.Execute(ctx, initConsensusParamsTable)
			return err
		},
	}

	return versioning.Upgrade(ctx, db, chainSchemaName, upgradeFns, chainStoreVersion)
}

// GetChainState returns height and app hash from the chain state store.
// If there is no recorded data, height will be -1 and app hash nil.
func GetChainState(ctx context.Context, db sql.Executor) (int64, []byte, error) {
	res, err := db.Execute(ctx, getChainState)
	if err != nil {
		return 0, nil, err
	}

	switch n := len(res.Rows); n {
	case 0:
		return -1, nil, nil // fresh DB
	case 1:
	default:
		return 0, nil, fmt.Errorf("expected at most one row, got %d", n)
	}

	row := res.Rows[0]
	if len(row) != 2 {
		return 0, nil, fmt.Errorf("expected two columns, got %d", len(row))
	}

	height, ok := sql.Int64(row[0])
	if !ok {
		return 0, nil, fmt.Errorf("invalid type for height (%T)", res.Rows[0][0])
	}

	appHash, ok := row[1].([]byte)
	if !ok {
		return 0, nil, fmt.Errorf("expected bytes for apphash, got %T", row[1])
	}

	return height, slices.Clone(appHash), nil
}

// SetChainState will update the current height and app hash.
func SetChainState(ctx context.Context, db sql.TxMaker, height int64, appHash []byte) error {
	tx, err := db.BeginTx(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	// attempt UPDATE
	res, err := tx.Execute(ctx, setChainState, height, appHash)
	if err != nil {
		return err
	}
	// If no rows updated, meaning empty table, do INSERT
	if res.Status.RowsAffected == 0 {
		_, err = tx.Execute(ctx, insertChainState, height, appHash)
	}
	if err != nil {
		return err
	}

	return tx.Commit(ctx)
}

// StoreParams stores the consensus params in the store.
func StoreParams(ctx context.Context, db sql.TxMaker, params *common.NetworkParameters) error {
	tx, err := db.BeginTx(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	buf := make([]byte, 8)
	binary.LittleEndian.PutUint64(buf, uint64(params.MaxBlockSize))
	_, err = tx.Execute(ctx, upsertParam, maxBlockSizeKey, buf)
	if err != nil {
		return err
	}

	binary.LittleEndian.PutUint64(buf, uint64(params.JoinExpiry))
	_, err = tx.Execute(ctx, upsertParam, joinExpiryKey, buf)
	if err != nil {
		return err
	}

	binary.LittleEndian.PutUint64(buf, uint64(params.VoteExpiry))
	_, err = tx.Execute(ctx, upsertParam, voteExpiryKey, buf)
	if err != nil {
		return err
	}

	buf = make([]byte, 1)
	if params.DisabledGasCosts {
		buf[0] = 1
	}
	_, err = tx.Execute(ctx, upsertParam, disabledGasKey, buf)
	if err != nil {
		return err
	}

	return tx.Commit(ctx)
}

// StoreDiff stores the difference between two sets of consensus params.
// If the parameters are equal, no action is taken.
func StoreDiff(ctx context.Context, db sql.TxMaker, original, new *common.NetworkParameters) error {
	diff := diff(original, new)
	if len(diff) == 0 {
		return nil
	}

	tx, err := db.BeginTx(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	for param, value := range diff {
		_, err = tx.Execute(ctx, upsertParam, param, value)
		if err != nil {
			return err
		}
	}

	return tx.Commit(ctx)
}

var ErrParamsNotFound = fmt.Errorf("params not found")

// LoadParams loads the consensus params from the store.
func LoadParams(ctx context.Context, db sql.Executor) (*common.NetworkParameters, error) {
	res, err := db.Execute(ctx, getParams)
	if err != nil {
		return nil, err
	}

	if len(res.Rows) == 0 {
		return nil, ErrParamsNotFound
	}

	if len(res.Rows) != 4 {
		return nil, fmt.Errorf("expected four rows, got %d", len(res.Rows))
	}

	params := &common.NetworkParameters{}
	for _, row := range res.Rows {
		if len(row) != 2 {
			return nil, fmt.Errorf("expected two columns, got %d", len(row))
		}

		param, ok := row[0].(string)
		if !ok {
			return nil, fmt.Errorf("expected string for param name, got %T", row[0])
		}

		value, ok := row[1].([]byte)
		if !ok {
			return nil, fmt.Errorf("expected bytes for param value, got %T", row[1])
		}

		switch param {
		case maxBlockSizeKey:
			params.MaxBlockSize = int64(binary.LittleEndian.Uint64(value))
		case joinExpiryKey:
			params.JoinExpiry = int64(binary.LittleEndian.Uint64(value))
		case voteExpiryKey:
			params.VoteExpiry = int64(binary.LittleEndian.Uint64(value))
		case disabledGasKey:
			params.DisabledGasCosts = value[0] == 1
		default:
			return nil, fmt.Errorf("internal bug: unknown param name: %s", param)
		}
	}

	return params, nil
}

// diff returns the difference between two sets of consensus params.
func diff(original, new *common.NetworkParameters) map[string][]byte {
	d := make(map[string][]byte)
	if original.MaxBlockSize != new.MaxBlockSize {
		buf := make([]byte, 8)
		binary.LittleEndian.PutUint64(buf, uint64(new.MaxBlockSize))
		d[maxBlockSizeKey] = buf
	}

	if original.JoinExpiry != new.JoinExpiry {
		buf := make([]byte, 8)
		binary.LittleEndian.PutUint64(buf, uint64(new.JoinExpiry))
		d[joinExpiryKey] = buf
	}

	if original.VoteExpiry != new.VoteExpiry {
		buf := make([]byte, 8)
		binary.LittleEndian.PutUint64(buf, uint64(new.VoteExpiry))
		d[voteExpiryKey] = buf
	}

	if original.DisabledGasCosts != new.DisabledGasCosts {
		buf := make([]byte, 1)
		if new.DisabledGasCosts {
			buf[0] = 1
		}
		d[disabledGasKey] = buf
	}

	return d
}

const (
	maxBlockSizeKey = `max_block_size`
	joinExpiryKey   = `join_expiry`
	voteExpiryKey   = `vote_expiry`
	disabledGasKey  = `disabled_gas_costs`
)
