package feegrant

import (
	"encoding/json"
	"fmt"
	"github.com/CosmWasm/wasmd/x/wasm"
	"github.com/archway-network/archway/x/gastracker"
	"github.com/archway-network/archway/x/gastracker/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/x/auth/ante"
	"github.com/cosmos/cosmos-sdk/x/auth/keeper"
)

type ProxyFeeGrantKeeper struct {
	underlyingFeeGrantKeeper ante.FeegrantKeeper
	wasmKeeper               wasm.Keeper
	gastrackingKeeper        gastracker.Keeper
	accountKeeper            keeper.AccountKeeper
}

func (p *ProxyFeeGrantKeeper) extractContractAddressAndMsg(msg sdk.Msg) (sdk.AccAddress, types.WasmMsg, error) {
	switch msg := msg.(type) {
	case *wasm.MsgExecuteContract:
		addr, err := sdk.AccAddressFromBech32(msg.Contract)
		if err != nil {
			return nil, types.WasmMsg{}, err
		}
		return addr, types.WasmMsg{
			Type: "Execute",
			Data: msg.Msg,
		}, nil
	case *wasm.MsgMigrateContract:
		addr, err := sdk.AccAddressFromBech32(msg.Contract)
		if err != nil {
			return nil, types.WasmMsg{}, err
		}
		return addr, types.WasmMsg{
			Type: "Execute",
			Data: msg.Msg,
		}, nil
	default:
		return nil, types.WasmMsg{}, fmt.Errorf("only contract invoking messages should be in the tx")
	}
}

func (p *ProxyFeeGrantKeeper) getContractAddressAndMsgs(msgs []sdk.Msg) (sdk.AccAddress, []*types.WasmMsg, error) {
	var txContractAddress sdk.AccAddress
	wasmMsgs := make([]*types.WasmMsg, len(msgs))
	for i, msg := range msgs {
		extractedAddress, wasmMsg, err := p.extractContractAddressAndMsg(msg)
		if err != nil {
			return nil, nil, err
		}
		if txContractAddress == nil {
			txContractAddress = extractedAddress
		} else {
			if !txContractAddress.Equals(extractedAddress) {
				return nil, nil, fmt.Errorf("only one contract should be called for the message")
			}
		}
		wasmMsgs[i] = &wasmMsg
	}
	return txContractAddress, wasmMsgs, nil
}

func (p *ProxyFeeGrantKeeper) checkAndDeductContractBalance(ctx sdk.Context, contractAddress sdk.AccAddress, fee sdk.Coins) error {
	metadata, err := p.gastrackingKeeper.GetContractSystemMetadata(ctx, contractAddress)
	if err != nil {
		return err
	}

	convertedFee := make(sdk.DecCoins, len(fee))
	for i := range fee {
		convertedFee[i] = sdk.NewDecCoinFromCoin(fee[i])
	}

	convertedInflationBalance := make(sdk.DecCoins, len(metadata.InflationBalance))
	for i := range metadata.InflationBalance {
		convertedInflationBalance[i] = *metadata.InflationBalance[i]
	}

	balance, isOverFlowed := convertedInflationBalance.SafeSub(convertedFee)
	if isOverFlowed {
		return fmt.Errorf("contract's reward is insufficient to cover for the fee")
	}

	convertedBalance := make([]*sdk.DecCoin, len(balance))
	for i := range balance {
		convertedBalance[i] = &balance[i]
	}
	metadata.InflationBalance = convertedBalance

	return p.gastrackingKeeper.SetContractSystemMetadata(ctx, contractAddress, metadata)
}

func (p *ProxyFeeGrantKeeper) UseGrantedFees(ctx sdk.Context, granter, grantee sdk.AccAddress, fee sdk.Coins, msgs []sdk.Msg) error {
	rewardAccumulatorAddress := p.accountKeeper.GetModuleAddress(gastracker.InflationRewardAccumulator)
	if rewardAccumulatorAddress == nil {
		return fmt.Errorf("FATAL INTERNAL: inflation reward accumulator does not exist")
	}
	if !granter.Equals(rewardAccumulatorAddress) {
		return p.underlyingFeeGrantKeeper.UseGrantedFees(ctx, granter, grantee, fee, msgs)
	}

	contractAddress, wasmMsgs, err := p.getContractAddressAndMsgs(msgs)
	if err != nil {
		return err
	}

	err = p.checkAndDeductContractBalance(ctx, contractAddress, fee)
	if err != nil {
		return err
	}

	protoFees := make([]*sdk.Coin, len(fee))
	for i := range protoFees {
		protoFees[i] = &fee[i]
	}

	sudoMsg := types.ContractValidFeeGranteeMsg{
		Grantee:       contractAddress.String(),
		GasFeeToGrant: protoFees,
		Msgs:          wasmMsgs,
	}

	jsonMsg, err := json.Marshal(sudoMsg)
	if err != nil {
		return err
	}

	_, err = p.wasmKeeper.Sudo(ctx, contractAddress, jsonMsg)
	if err != nil {
		return err
	}

	return p.gastrackingKeeper.MarkCurrentTxNonEligibleForReward(ctx)
}

func NewProxyFeeGrantKeeper(underlyingKeeper ante.FeegrantKeeper, wasmKeeper wasm.Keeper, gastrackingKeeper gastracker.Keeper) *ProxyFeeGrantKeeper {
	return &ProxyFeeGrantKeeper{
		wasmKeeper:               wasmKeeper,
		gastrackingKeeper:        gastrackingKeeper,
		underlyingFeeGrantKeeper: underlyingKeeper,
	}
}
