package feegrant

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"github.com/CosmWasm/wasmd/x/wasm"
	"github.com/archway-network/archway/x/gastracker"
	"github.com/archway-network/archway/x/gastracker/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/x/auth/ante"
)

func encodeHeightCounter(height int64, counter uint32) []byte {
	b := make([]byte, 4)
	binary.BigEndian.PutUint32(b, counter)
	return append(sdk.Uint64ToBigEndian(uint64(height)), b...)
}

func decodeHeightCounter(bz []byte) (int64, uint32) {
	return int64(sdk.BigEndianToUint64(bz[0:8])), binary.BigEndian.Uint32(bz[8:])
}

type GasTrackingKeeperFeeGrantView interface {
	MarkCurrentTxNonEligibleForReward(ctx sdk.Context) error
	GetContractSystemMetadata(ctx sdk.Context, address sdk.AccAddress) (types.ContractInstanceSystemMetadata, error)
	SetContractSystemMetadata(ctx sdk.Context, address sdk.AccAddress, metadata types.ContractInstanceSystemMetadata) error
}

type WasmKeeperFeeGrantView interface {
	Sudo(ctx sdk.Context, contractAddress sdk.AccAddress, msg []byte) ([]byte, error)
}

type AccountKeeperFeeGrantView interface {
	GetModuleAddress(moduleName string) sdk.AccAddress
}

type ProxyFeeGrantKeeper struct {
	underlyingFeeGrantKeeper ante.FeegrantKeeper
	wasmKeeper               WasmKeeperFeeGrantView
	gastrackingKeeper        GasTrackingKeeperFeeGrantView
	accountKeeper            AccountKeeperFeeGrantView
}

func (p *ProxyFeeGrantKeeper) extractContractAddressAndMsg(msg sdk.Msg) (sdk.AccAddress, types.WasmMsg, error) {
	switch msg := msg.(type) {
	case *wasm.MsgExecuteContract:
		addr, err := sdk.AccAddressFromBech32(msg.Contract)
		if err != nil {
			return nil, types.WasmMsg{}, err
		}
		return addr, types.WasmMsg{
			Type: types.WasmMsgType_WASM_MSG_TYPE_EXECUTE,
			Data: msg.Msg,
		}, nil
	case *wasm.MsgMigrateContract:
		addr, err := sdk.AccAddressFromBech32(msg.Contract)
		if err != nil {
			return nil, types.WasmMsg{}, err
		}
		return addr, types.WasmMsg{
			Type: types.WasmMsgType_WASM_MSG_TYPE_MIGRATE,
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
	if len(wasmMsgs) == 0 {
		return nil, nil, fmt.Errorf("FATAL INTERNAL: no message passed")
	}
	return txContractAddress, wasmMsgs, nil
}

func (p *ProxyFeeGrantKeeper) checkAndDeductContractBalance(ctx sdk.Context, contractAddress sdk.AccAddress, fee sdk.Coins, metadata types.ContractInstanceSystemMetadata) error {
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

func (p *ProxyFeeGrantKeeper) isRequestRateLimited(ctx sdk.Context, metadata types.ContractInstanceSystemMetadata) (bool, types.ContractInstanceSystemMetadata) {
	if ctx.IsCheckTx() || ctx.IsReCheckTx() {
		return false, metadata
	}

	if ctx.BlockHeight()%3 != 0 {
		return true, metadata
	}

	if metadata.BlockTxCounter == nil {
		metadata.BlockTxCounter = encodeHeightCounter(ctx.BlockHeight(), 1)
		return false, metadata
	}

	height, txCounter := decodeHeightCounter(metadata.BlockTxCounter)
	if height != ctx.BlockHeight() {
		metadata.BlockTxCounter = encodeHeightCounter(ctx.BlockHeight(), 1)
		return false, metadata
	}

	if txCounter > 2 {
		return true, metadata
	}

	metadata.BlockTxCounter = encodeHeightCounter(ctx.BlockHeight(), txCounter+1)
	return false, metadata
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

	metadata, err := p.gastrackingKeeper.GetContractSystemMetadata(ctx, contractAddress)
	if err != nil {
		return err
	}

	isRateLimited, metadata := p.isRequestRateLimited(ctx, metadata)
	if isRateLimited {
		return fmt.Errorf("fee grant is rate limited, please try again")
	}

	err = p.checkAndDeductContractBalance(ctx, contractAddress, fee, metadata)
	if err != nil {
		return err
	}

	protoFees := make([]*sdk.Coin, len(fee))
	for i := range protoFees {
		protoFees[i] = &fee[i]
	}

	sudoMsg := types.ContractValidFeeGranteeMsg{
		Grantee:       grantee.String(),
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

func NewProxyFeeGrantKeeper(underlyingKeeper ante.FeegrantKeeper, wasmKeeper WasmKeeperFeeGrantView, gastrackingKeeper GasTrackingKeeperFeeGrantView, accountKeeper AccountKeeperFeeGrantView) *ProxyFeeGrantKeeper {
	return &ProxyFeeGrantKeeper{
		wasmKeeper:               wasmKeeper,
		gastrackingKeeper:        gastrackingKeeper,
		underlyingFeeGrantKeeper: underlyingKeeper,
		accountKeeper:            accountKeeper,
	}
}
