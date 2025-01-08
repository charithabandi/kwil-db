package account

import (
	"context"
	"errors"
	"fmt"

	"github.com/kwilteam/kwil-db/app/shared/display"
	"github.com/kwilteam/kwil-db/cmd/kwil-cli/client"
	"github.com/kwilteam/kwil-db/cmd/kwil-cli/config"
	clientType "github.com/kwilteam/kwil-db/core/client/types"
	"github.com/kwilteam/kwil-db/core/crypto/auth"
	"github.com/kwilteam/kwil-db/core/types"
	"github.com/spf13/cobra"
)

func balanceCmd() *cobra.Command {
	var pending bool
	cmd := &cobra.Command{
		Use:   "balance",
		Short: "Gets an account's balance and nonce",
		Long:  `Gets an account's balance and nonce.`,
		Args:  cobra.MaximumNArgs(1), // no args means own account
		RunE: func(cmd *cobra.Command, args []string) error {
			var acctID string
			var clientFlags uint8
			if len(args) > 0 {
				clientFlags = client.WithoutPrivateKey
				acctID = args[0]
			} // else use our account from the signer

			return client.DialClient(cmd.Context(), cmd, clientFlags, func(ctx context.Context, cl clientType.Client, conf *config.KwilCliConfig) error {
				if len(args) == 0 {
					if cl.Signer() == nil {
						return display.PrintErr(cmd, errors.New("no account ID provided and no signer set"))
					}

					ident, err := auth.Secp25k1Authenticator{}.Identifier(cl.Signer().CompactID())
					if err != nil {
						return display.PrintErr(cmd, fmt.Errorf("failed to get identifier: %w", err))
					}
					acctID = ident
					if len(acctID) == 0 {
						return display.PrintErr(cmd, errors.New("empty account ID"))
					}
				}
				status := types.AccountStatusLatest
				if pending {
					status = types.AccountStatusPending
				}
				acct, err := cl.GetAccount(ctx, acctID, status)
				if err != nil {
					return display.PrintErr(cmd, fmt.Errorf("get account failed: %w", err))
				}
				// NOTE: empty acct.Identifier means it doesn't even have a record
				// on the network. Perhaps we convey that to the caller? Their
				// balance is zero regardless, assuming it's the correct acct ID.
				resp := (*respAccount)(acct)
				return display.PrintCmd(cmd, resp)
			})

		},
	}

	cmd.Flags().BoolVar(&pending, "pending", false, "reflect pending updates from mempool (default is confirmed only)")

	return cmd
}
