package setup

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"reflect"
	"strconv"
	"strings"
	"time"

	"github.com/kwilteam/kwil-db/app/shared/display"
	root "github.com/kwilteam/kwil-db/cmd/kwil-cli/cmds"
	"github.com/kwilteam/kwil-db/cmd/kwil-cli/cmds/database"
	clientImpl "github.com/kwilteam/kwil-db/core/client"
	client "github.com/kwilteam/kwil-db/core/client/types"
	"github.com/kwilteam/kwil-db/core/crypto"
	"github.com/kwilteam/kwil-db/core/crypto/auth"
	"github.com/kwilteam/kwil-db/core/types"
	"github.com/spf13/cobra"
)

type jsonRPCCLIDriver struct {
	provider     string
	privateKey   crypto.PrivateKey
	chainID      string
	usingGateway bool
	logFunc      logFunc
	cobraCmd     *cobra.Command
}

func newKwilCI(ctx context.Context, endpoint string, l logFunc, opts *ClientOptions) (JSONRPCClient, error) {
	if opts == nil {
		opts = &ClientOptions{}
	}
	opts.ensureDefaults()

	return &jsonRPCCLIDriver{
		provider:     endpoint,
		privateKey:   opts.PrivateKey.(*crypto.Secp256k1PrivateKey),
		chainID:      opts.ChainID,
		usingGateway: opts.UsingKGW,
		logFunc:      l,
		cobraCmd:     root.NewRootCmd(),
	}, nil
}

// cmd executes a kwil-cli command and unmarshals the result into res.
// It logically should be a method on jsonRPCCLIDriver, but it can't because of the generic type T.
func cmd[T any](j *jsonRPCCLIDriver, ctx context.Context, res T, args ...string) error {
	flags := []string{"--provider", j.provider, "--private-key", hex.EncodeToString(j.privateKey.Bytes()), "--output", "json", "--assume-yes", "--silence", "--chain-id", j.chainID}

	buf := new(bytes.Buffer)

	cmd := root.NewRootCmd()
	cmd.SetOut(buf)
	cmd.SetArgs(append(flags, args...))
	err := cmd.ExecuteContext(ctx)
	if err != nil {
		return err
	}

	if buf.Len() == 0 {
		return fmt.Errorf("no output from command")
	}

	fmt.Println("Running Command ", `/app/kwil-cli `+strings.Join(args, " "), " with output ", buf.String())

	d := display.MessageReader[T]{
		Result: res,
	}

	bts := buf.Bytes()
	err = json.Unmarshal(bts, &d)
	if err != nil {
		return fmt.Errorf("unmarshal error: %w", err)
	}

	if d.Error != "" {
		return fmt.Errorf("error in command: %s", d.Error)
	}

	return nil
}

func (j *jsonRPCCLIDriver) PrivateKey() crypto.PrivateKey {
	return j.privateKey
}

func (j *jsonRPCCLIDriver) PublicKey() crypto.PublicKey {
	return j.privateKey.Public()
}

func (j *jsonRPCCLIDriver) Signer() auth.Signer {
	return &auth.Secp256k1Signer{Secp256k1PrivateKey: *j.privateKey.(*crypto.Secp256k1PrivateKey)}
}

func (j *jsonRPCCLIDriver) Identifier() string {
	ident, err := auth.Secp25k1Authenticator{}.Identifier(j.privateKey.Public().Bytes())
	if err != nil {
		panic(err)
	}

	return ident
}

func (j *jsonRPCCLIDriver) Call(ctx context.Context, namespace string, action string, inputs []any) (*types.CallResult, error) {
	args := []string{"database", "call", "--logs"}
	if j.usingGateway {
		args = append(args, "--authenticate")
	}
	params, err := j.buildActionParams(ctx, namespace, action, inputs)
	if err != nil {
		return nil, err
	}

	args = append(args, params...)

	r := &types.CallResult{}
	err = cmd(j, ctx, r, args...)
	if err != nil {
		return nil, err
	}

	return r, nil
}

// buildActionParams takes a list of arguments to an action, finds the name of their parameters, and returns them as a list
// of strings that can be used in a CLI command
func (j *jsonRPCCLIDriver) buildActionParams(ctx context.Context, namespace string, action string, inputs []any) ([]string, error) {
	params, err := database.GetParamList(ctx, j.Query, namespace, action)
	if err != nil {
		return nil, err
	}

	// there can be less inputs than params, but not more.
	// Any params not included or left nil should not get passed to the action
	if len(inputs) > len(params) {
		return nil, fmt.Errorf("too many arguments for action %s.%s", namespace, action)
	}

	args := []string{action}
	for i, in := range inputs {
		if in == nil {
			continue
		}

		args = append(args, delimitNameAndArg(params[i].Name, in))
	}

	if namespace != "" {
		args = append(args, "--namespace", namespace)
	}

	return args, nil
}

func delimitNameAndArg(name string, arg any) string {
	return name + ":" + stringifyCLIArg(arg)
}

func (j *jsonRPCCLIDriver) ChainID() string {
	i, err := j.ChainInfo(context.Background())
	if err != nil {
		panic(err)
	}

	return i.ChainID
}

func (j *jsonRPCCLIDriver) ChainInfo(ctx context.Context) (*types.ChainInfo, error) {
	r := &types.ChainInfo{}
	err := cmd(j, ctx, r, "utils", "chain-info")
	if err != nil {
		return nil, err
	}

	return r, nil
}

func (j *jsonRPCCLIDriver) Execute(ctx context.Context, namespace string, action string, tuples [][]any, opts ...client.TxOpt) (types.Hash, error) {
	if len(tuples) > 1 {
		// TODO: we could fix this by supporting the batch command in the driver.
		// I will come back to this
		return types.Hash{}, fmt.Errorf("only one tuple is supported in cli driver")
	}

	args := []string{"database", "execute"}
	if len(tuples) == 1 {
		res, err := j.buildActionParams(ctx, namespace, action, tuples[0])
		if err != nil {
			return types.Hash{}, err
		}
		args = append(args, res...)
	}
	// if 0 len tuples, no args are needed

	return j.exec(ctx, args, opts...)
}

func stringifyCLIArg(a any) string {
	// if it is an array, we need to delimit it with commas
	typeof := reflect.TypeOf(a)
	if typeof.Kind() == reflect.Slice && typeof.Elem().Kind() != reflect.Uint8 {
		slice := reflect.ValueOf(a)
		args := make([]string, slice.Len())
		for i := range slice.Len() {
			args[i] = stringifyCLIArg(slice.Index(i).Interface())
		}
		return strings.Join(args, ",")
	}

	switch t := a.(type) {
	case string:
		return t
	case []byte:
		return database.FormatByteEncoding(t)
	case fmt.Stringer:
		return t.String()
	default:
		return fmt.Sprintf("%v", t)
	}
}

func (j *jsonRPCCLIDriver) ExecuteSQL(ctx context.Context, sql string, params map[string]any, opts ...client.TxOpt) (types.Hash, error) {
	args := append([]string{"database", "execute"}, "--sql", sql)
	for k, v := range params {
		args = append(args, k+":"+stringifyCLIArg(v))
	}

	return j.exec(ctx, args, opts...)
}

// exec executes the kwil-cli database execute command
func (j *jsonRPCCLIDriver) exec(ctx context.Context, args []string, opts ...client.TxOpt) (types.Hash, error) {
	opts2 := client.GetTxOpts(opts)
	if opts2.Fee != nil {
		return types.Hash{}, fmt.Errorf("fee tx opts is not supported in cli driver")
	}
	if opts2.Nonce != 0 {
		args = append(args, "--nonce", strconv.FormatInt(opts2.Nonce, 10))
	}

	if opts2.SyncBcast {
		r := &display.TxHashResponse{}
		err := cmd(j, ctx, r, append(args, "--sync")...)
		if err != nil {
			return types.Hash{}, err
		}

		return r.TxHash, nil
	}

	// otherwise, we have a different structure
	r := display.TxHashResponse{}
	err := cmd(j, ctx, &r, args...)
	if err != nil {
		return types.Hash{}, err
	}

	return r.TxHash, nil
}

// printWithSync will
type respAccount struct {
	Identifier types.HexBytes `json:"identifier"`
	KeyType    string         `json:"key_type"`
	Balance    string         `json:"balance"`
	Nonce      int64          `json:"nonce"`
}

func (j *jsonRPCCLIDriver) GetAccount(ctx context.Context, acct *types.AccountID, status types.AccountStatus) (*types.Account, error) {
	r := &respAccount{}

	args := []string{"account", "balance", hex.EncodeToString(acct.Identifier), acct.KeyType.String()}
	if status == types.AccountStatusPending {
		args = append(args, "--pending")
	}

	err := cmd(j, ctx, r, args...)
	if err != nil {
		return nil, err
	}

	bal, ok := big.NewInt(0).SetString(r.Balance, 10)
	if !ok {
		return nil, errors.New("invalid decimal string balance")
	}

	return &types.Account{
		ID:      acct,
		Balance: bal,
		Nonce:   r.Nonce,
	}, nil
}

func (j *jsonRPCCLIDriver) Ping(ctx context.Context) (string, error) {
	var r string
	err := cmd(j, ctx, &r, "utils", "ping")
	return r, err
}

func (j *jsonRPCCLIDriver) Query(ctx context.Context, query string, params map[string]any) (*types.QueryResult, error) {
	args := []string{"database", "query", query}
	for k, v := range params {
		args = append(args, k+":"+stringifyCLIArg(v))
	}

	r := &types.QueryResult{}
	err := cmd(j, ctx, r, args...)
	if err != nil {
		return nil, err
	}

	return r, nil
}

func (j *jsonRPCCLIDriver) TxQuery(ctx context.Context, txHash types.Hash) (*types.TxQueryResponse, error) {
	r := &types.TxQueryResponse{}
	err := cmd(j, ctx, r, "utils", "query-tx", txHash.String())
	if err != nil {
		return nil, err
	}

	return r, nil
}

func (j *jsonRPCCLIDriver) TxSuccess(ctx context.Context, txHash types.Hash) error {
	res, err := j.TxQuery(ctx, txHash)
	if err != nil {
		return err
	}

	if res.Height < 0 {
		return ErrTxNotConfirmed
	}

	if res.Result != nil && res.Result.Code != 0 {
		return fmt.Errorf("tx failed: %v", res.Result)
	}

	return nil
}

func (j *jsonRPCCLIDriver) WaitTx(ctx context.Context, txHash types.Hash, interval time.Duration) (*types.TxQueryResponse, error) {
	return clientImpl.WaitForTx(ctx, j.TxQuery, txHash, interval)
}

func (j *jsonRPCCLIDriver) Transfer(ctx context.Context, to *types.AccountID, amount *big.Int, opts ...client.TxOpt) (types.Hash, error) {
	return j.exec(ctx, []string{"account", "transfer", to.Identifier.String(), to.KeyType.String(), amount.String()})
}

func (j *jsonRPCCLIDriver) TransferAmt(ctx context.Context, to *types.AccountID, amt *big.Int, opts ...client.TxOpt) (types.Hash, error) {
	return j.Transfer(ctx, to, amt, opts...)
}

func (j *jsonRPCCLIDriver) AccountBalance(ctx context.Context, identifier string) (*big.Int, error) {
	r := &respAccount{}
	err := cmd(j, ctx, r, "account", "balance", identifier)
	if err != nil {
		return nil, err
	}

	bal, ok := big.NewInt(0).SetString(r.Balance, 10)
	if !ok {
		return nil, errors.New("invalid decimal string balance")
	}
	return bal, nil
}
