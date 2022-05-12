package feegrant

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"github.com/CosmWasm/wasmd/x/wasm"
	"github.com/archway-network/archway/x/gastracker"
	gstTypes "github.com/archway-network/archway/x/gastracker/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/x/auth/types"
	"github.com/stretchr/testify/require"
	"testing"
)

var _ GasTrackingKeeperFeeGrantView = &testGasTrackingKeeper{}

type setContractMetadataLog struct {
	Address  sdk.AccAddress
	Metadata gstTypes.ContractInstanceSystemMetadata
}

type testGasTrackingKeeper struct {
	FailTxNonEligibleCall bool
	InflationBalance      []*sdk.DecCoin

	TxNonElibigleCallLogs       []string
	GetContractMetadataCallLogs []sdk.AccAddress
	SetContractMetadataCallLogs []setContractMetadataLog
}

func (t *testGasTrackingKeeper) MarkCurrentTxNonEligibleForReward(ctx sdk.Context) error {
	if t.FailTxNonEligibleCall {
		return fmt.Errorf("fail tx non eligible call")
	}
	t.TxNonElibigleCallLogs = append(t.TxNonElibigleCallLogs, "")
	return nil
}

func (t *testGasTrackingKeeper) GetContractSystemMetadata(ctx sdk.Context, address sdk.AccAddress) (gstTypes.ContractInstanceSystemMetadata, error) {
	t.GetContractMetadataCallLogs = append(t.GetContractMetadataCallLogs, address)

	return gstTypes.ContractInstanceSystemMetadata{
		InflationBalance: t.InflationBalance,
	}, nil
}

func (t *testGasTrackingKeeper) SetContractSystemMetadata(ctx sdk.Context, address sdk.AccAddress, metadata gstTypes.ContractInstanceSystemMetadata) error {
	t.SetContractMetadataCallLogs = append(t.SetContractMetadataCallLogs, setContractMetadataLog{
		Address:  address,
		Metadata: metadata,
	})

	return nil
}

func (t *testGasTrackingKeeper) ResetLogs() {
	t.TxNonElibigleCallLogs = nil
	t.GetContractMetadataCallLogs = nil
	t.SetContractMetadataCallLogs = nil
}

type sudoCallLog struct {
	ContractAddress sdk.AccAddress
	Data            []byte
}

type testWasmKeeper struct {
	FailSudoCall bool
	SudoCallLogs []sudoCallLog
}

func (t *testWasmKeeper) Sudo(ctx sdk.Context, contractAddress sdk.AccAddress, data []byte) ([]byte, error) {
	t.SudoCallLogs = append(t.SudoCallLogs, sudoCallLog{
		ContractAddress: contractAddress,
		Data:            data,
	})

	if t.FailSudoCall {
		return nil, fmt.Errorf("fail sudo call")
	}
	return nil, nil
}

func (t *testWasmKeeper) ResetLogs() {
	t.SudoCallLogs = nil
}

type testAccountKeeper struct {
	FailModuleAddressCall bool
	ModuleCallAddressLogs []string
	Address               sdk.AccAddress
}

func (t *testAccountKeeper) GetModuleAddress(name string) sdk.AccAddress {
	t.ModuleCallAddressLogs = append(t.ModuleCallAddressLogs, name)
	if t.FailModuleAddressCall {
		return nil
	}
	return t.Address
}

func (t *testAccountKeeper) ResetLogs() {
	t.ModuleCallAddressLogs = nil
}

type useGrantedFeeCallLog struct {
	Granter sdk.AccAddress
	Grantee sdk.AccAddress
	Fee     sdk.Coins
	Msgs    []sdk.Msg
}

type testUnderlyingFeeGrantKeeper struct {
	FailUseGrantedFeeCall bool
	UseGrantedFeeCallLog  []useGrantedFeeCallLog
}

func (t *testUnderlyingFeeGrantKeeper) ResetLogs() {
	t.UseGrantedFeeCallLog = nil
}

func (t *testUnderlyingFeeGrantKeeper) UseGrantedFees(ctx sdk.Context, granter, grantee sdk.AccAddress, fee sdk.Coins, msgs []sdk.Msg) error {
	t.UseGrantedFeeCallLog = append(t.UseGrantedFeeCallLog, useGrantedFeeCallLog{
		Granter: granter,
		Grantee: grantee,
		Fee:     fee,
		Msgs:    msgs,
	})

	if t.FailUseGrantedFeeCall {
		return fmt.Errorf("fail use granted fee call")
	}

	return nil
}

func GenerateRandomAccAddress() sdk.AccAddress {
	var address sdk.AccAddress = make([]byte, 20)
	_, err := rand.Read(address)
	if err != nil {
		panic(err)
	}
	return address
}

func TestProxyFeeGrant(t *testing.T) {
	accountKeeper := testAccountKeeper{
		FailModuleAddressCall: true,
	}

	wasmKeeper := testWasmKeeper{
		FailSudoCall: true,
	}

	inflationBalance := make([]*sdk.DecCoin, 3)
	testCoinBal := sdk.NewDecCoinFromDec("test", sdk.MustNewDecFromStr("1.2022"))
	inflationBalance[0] = &testCoinBal
	test2CoinBal := sdk.NewDecCoinFromDec("test2", sdk.MustNewDecFromStr("2.1"))
	inflationBalance[1] = &test2CoinBal
	test3CoinBal := sdk.NewDecCoinFromDec("test3", sdk.MustNewDecFromStr("4.5"))
	inflationBalance[2] = &test3CoinBal

	normalFee := make([]sdk.Coin, 2)
	normalFee[0] = sdk.NewInt64Coin("test", 1)
	normalFee[1] = sdk.NewInt64Coin("test2", 1)

	normalFeeStorage := make([]*sdk.Coin, 2)
	normalFeeStorage[0] = &normalFee[0]
	normalFeeStorage[1] = &normalFee[1]

	normalFeeBalance := make([]*sdk.DecCoin, 3)
	balanceTestCoinBal := sdk.NewDecCoinFromDec("test", sdk.MustNewDecFromStr("0.2022"))
	normalFeeBalance[0] = &balanceTestCoinBal
	balanceTest2CoinBal := sdk.NewDecCoinFromDec("test2", sdk.MustNewDecFromStr("1.1"))
	normalFeeBalance[1] = &balanceTest2CoinBal
	balanceTest3CoinBal := sdk.NewDecCoinFromDec("test3", sdk.MustNewDecFromStr("4.5"))
	normalFeeBalance[2] = &balanceTest3CoinBal

	highFee := make([]sdk.Coin, 2)
	highFee[0] = sdk.NewInt64Coin("test2", 3)
	highFee[1] = sdk.NewInt64Coin("test3", 1)

	granteeAddress := GenerateRandomAccAddress()
	dummyGranter := GenerateRandomAccAddress()

	contractAddress1 := GenerateRandomAccAddress()
	contractAddress2 := GenerateRandomAccAddress()

	gasTrackerKeeper := testGasTrackingKeeper{
		FailTxNonEligibleCall: true,
		InflationBalance:      inflationBalance,
	}

	underlyingFeeGrantKeeper := testUnderlyingFeeGrantKeeper{
		FailUseGrantedFeeCall: true,
	}

	validMsgs := []sdk.Msg{
		&wasm.MsgExecuteContract{
			Contract: contractAddress1.String(),
			Msg:      []byte{1},
		},
		&wasm.MsgMigrateContract{
			Contract: contractAddress1.String(),
			Msg:      []byte{2},
		},
		&wasm.MsgExecuteContract{
			Contract: contractAddress1.String(),
			Msg:      []byte{3},
		},
	}

	invalidMsgs1 := []sdk.Msg{
		&wasm.MsgExecuteContract{},
	}

	invalidMsgs2 := []sdk.Msg{
		&wasm.MsgExecuteContract{
			Contract: contractAddress1.String(),
		},
		&gstTypes.MsgSetContractMetadata{},
	}

	invalidMsgs3 := []sdk.Msg{
		&wasm.MsgMigrateContract{
			Contract: contractAddress1.String(),
		},
		&wasm.MsgExecuteContract{
			Contract: contractAddress2.String(),
		},
	}

	sudoData := gstTypes.ContractValidFeeGranteeMsg{
		Grantee:       granteeAddress.String(),
		GasFeeToGrant: normalFeeStorage,
		Msgs: []*gstTypes.WasmMsg{
			{
				Type: gstTypes.WasmMsgType_WASM_MSG_TYPE_EXECUTE,
				Data: []byte{1},
			},
			{
				Type: gstTypes.WasmMsgType_WASM_MSG_TYPE_MIGRATE,
				Data: []byte{2},
			},
			{
				Type: gstTypes.WasmMsgType_WASM_MSG_TYPE_EXECUTE,
				Data: []byte{3},
			},
		},
	}

	sudoDataSerialized, err := json.Marshal(sudoData)
	require.NoError(t, err, "We should be able to marshal json")

	// Test 1: The reward accumulator module account is not registered
	proxyFeeGrantKeeper := NewProxyFeeGrantKeeper(&underlyingFeeGrantKeeper, &wasmKeeper, &gasTrackerKeeper, &accountKeeper)
	err = proxyFeeGrantKeeper.UseGrantedFees(sdk.Context{}, dummyGranter, granteeAddress, normalFee, validMsgs)
	require.EqualError(t, err, "FATAL INTERNAL: inflation reward accumulator does not exist", "call should error because reward accumulator does not exists")
	require.Equal(t, 1, len(accountKeeper.ModuleCallAddressLogs), "there should be one call to get the module address")
	require.Equal(t, gastracker.InflationRewardAccumulator, accountKeeper.ModuleCallAddressLogs[0], "the module name should be of the accumulator")

	accountKeeper.ResetLogs()

	// Test 2: The Granter does not match the module account
	accountKeeper.FailModuleAddressCall = false
	accountKeeper.Address = types.NewModuleAddress("test")
	underlyingFeeGrantKeeper.FailUseGrantedFeeCall = false
	err = proxyFeeGrantKeeper.UseGrantedFees(sdk.Context{}, dummyGranter, granteeAddress, normalFee, validMsgs)
	require.NoError(t, err, "There should not be any error")
	require.Equal(t, 1, len(accountKeeper.ModuleCallAddressLogs), "there should be one call to get the module address")
	require.Equal(t, gastracker.InflationRewardAccumulator, accountKeeper.ModuleCallAddressLogs[0], "the module name should be of the accumulator")
	require.Equal(t, 1, len(underlyingFeeGrantKeeper.UseGrantedFeeCallLog), "There should be one call to underlying fee granter")
	require.Equal(t, useGrantedFeeCallLog{
		Granter: dummyGranter,
		Grantee: granteeAddress,
		Fee:     normalFee,
		Msgs:    validMsgs,
	}, underlyingFeeGrantKeeper.UseGrantedFeeCallLog[0], "call arguments should match")

	accountKeeper.ResetLogs()
	underlyingFeeGrantKeeper.ResetLogs()

	// Test 3: Invalid Msgs (Msg's contract address field is empty) are passed
	err = proxyFeeGrantKeeper.UseGrantedFees(sdk.Context{}, accountKeeper.Address, granteeAddress, normalFee, invalidMsgs1)
	require.EqualError(t, err, "empty address string is not allowed", "There should be an error regarding empty bech32 string")
	require.Equal(t, 1, len(accountKeeper.ModuleCallAddressLogs), "there should be one call to get the module address")
	require.Equal(t, gastracker.InflationRewardAccumulator, accountKeeper.ModuleCallAddressLogs[0], "the module name should be of the accumulator")

	require.Equal(t, 0, len(underlyingFeeGrantKeeper.UseGrantedFeeCallLog), "there should be zero call to underlying fee grant keeper")
	require.Equal(t, 0, len(gasTrackerKeeper.GetContractMetadataCallLogs), "there should be zero call to get contract metadata")
	require.Equal(t, 0, len(gasTrackerKeeper.SetContractMetadataCallLogs), "there should be zero call to set contract metadata")
	require.Equal(t, 0, len(wasmKeeper.SudoCallLogs), "there should be zero sudo call to contract")
	require.Equal(t, 0, len(gasTrackerKeeper.TxNonElibigleCallLogs), "there should be zero call to set tx non eligibility")

	accountKeeper.ResetLogs()

	// Test 4: Invalid Msgs (One of the msg is of unsupported type) are passed
	err = proxyFeeGrantKeeper.UseGrantedFees(sdk.Context{}, accountKeeper.Address, granteeAddress, normalFee, invalidMsgs2)
	require.EqualError(t, err, "only contract invoking messages should be in the tx", "There should be an error regarding non contract invoking message")
	require.Equal(t, 1, len(accountKeeper.ModuleCallAddressLogs), "there should be one call to get the module address")
	require.Equal(t, gastracker.InflationRewardAccumulator, accountKeeper.ModuleCallAddressLogs[0], "the module name should be of the accumulator")

	require.Equal(t, 0, len(underlyingFeeGrantKeeper.UseGrantedFeeCallLog), "there should be zero call to underlying fee grant keeper")
	require.Equal(t, 0, len(gasTrackerKeeper.GetContractMetadataCallLogs), "there should be zero call to get contract metadata")
	require.Equal(t, 0, len(gasTrackerKeeper.SetContractMetadataCallLogs), "there should be zero call to set contract metadata")
	require.Equal(t, 0, len(wasmKeeper.SudoCallLogs), "there should be zero sudo call to contract")
	require.Equal(t, 0, len(gasTrackerKeeper.TxNonElibigleCallLogs), "there should be zero call to set tx non eligibility")

	accountKeeper.ResetLogs()

	// Test 5: Invalid Msgs (With different contract addresses) are passed
	err = proxyFeeGrantKeeper.UseGrantedFees(sdk.Context{}, accountKeeper.Address, granteeAddress, normalFee, invalidMsgs3)
	require.EqualError(t, err, "only one contract should be called for the message", "There should be an error regarding calling multiple contracts")
	require.Equal(t, 1, len(accountKeeper.ModuleCallAddressLogs), "there should be one call to get the module address")
	require.Equal(t, gastracker.InflationRewardAccumulator, accountKeeper.ModuleCallAddressLogs[0], "the module name should be of the accumulator")

	require.Equal(t, 0, len(underlyingFeeGrantKeeper.UseGrantedFeeCallLog), "there should be zero call to underlying fee grant keeper")
	require.Equal(t, 0, len(gasTrackerKeeper.GetContractMetadataCallLogs), "there should be zero call to get contract metadata")
	require.Equal(t, 0, len(gasTrackerKeeper.SetContractMetadataCallLogs), "there should be zero call to set contract metadata")
	require.Equal(t, 0, len(wasmKeeper.SudoCallLogs), "there should be zero sudo call to contract")
	require.Equal(t, 0, len(gasTrackerKeeper.TxNonElibigleCallLogs), "there should be zero call to set tx non eligibility")

	accountKeeper.ResetLogs()

	//Test 5: high fee
	err = proxyFeeGrantKeeper.UseGrantedFees(sdk.Context{}, accountKeeper.Address, granteeAddress, highFee, validMsgs)
	require.EqualError(t, err, "contract's reward is insufficient to cover for the fee", "There should be an error regarding high fee")
	require.Equal(t, 1, len(accountKeeper.ModuleCallAddressLogs), "there should be one call to get the module address")
	require.Equal(t, gastracker.InflationRewardAccumulator, accountKeeper.ModuleCallAddressLogs[0], "the module name should be of the accumulator")
	require.Equal(t, 1, len(gasTrackerKeeper.GetContractMetadataCallLogs), "there should be one call to get contract metadata")
	require.Equal(t, contractAddress1, gasTrackerKeeper.GetContractMetadataCallLogs[0], "the contract metadata call should be with correct address")

	require.Equal(t, 0, len(underlyingFeeGrantKeeper.UseGrantedFeeCallLog), "there should be zero call to underlying fee grant keeper")
	require.Equal(t, 0, len(gasTrackerKeeper.SetContractMetadataCallLogs), "there should be zero call to set contract metadata")
	require.Equal(t, 0, len(wasmKeeper.SudoCallLogs), "there should be zero sudo call to contract")
	require.Equal(t, 0, len(gasTrackerKeeper.TxNonElibigleCallLogs), "there should be zero call to set tx non eligibility")

	accountKeeper.ResetLogs()
	gasTrackerKeeper.ResetLogs()

	// Test 6: Normal Fee, but the contract denies access to funds
	err = proxyFeeGrantKeeper.UseGrantedFees(sdk.Context{}, accountKeeper.Address, granteeAddress, normalFee, validMsgs)
	require.EqualError(t, err, "fail sudo call", "contract should be denying access to the funds")
	require.Equal(t, 1, len(accountKeeper.ModuleCallAddressLogs), "there should be one call to get the module address")
	require.Equal(t, gastracker.InflationRewardAccumulator, accountKeeper.ModuleCallAddressLogs[0], "the module name should be of the accumulator")
	require.Equal(t, 1, len(gasTrackerKeeper.GetContractMetadataCallLogs), "there should be one call to get contract metadata")
	require.Equal(t, contractAddress1, gasTrackerKeeper.GetContractMetadataCallLogs[0], "the contract metadata call should be with correct address")
	require.Equal(t, 1, len(gasTrackerKeeper.SetContractMetadataCallLogs), "there should be one call to set contract metadata")
	require.Equal(t, setContractMetadataLog{
		Address: contractAddress1,
		Metadata: gstTypes.ContractInstanceSystemMetadata{
			InflationBalance: normalFeeBalance,
		},
	}, gasTrackerKeeper.SetContractMetadataCallLogs[0], "set contract metadata call should be with correct address and balance")
	require.Equal(t, 1, len(wasmKeeper.SudoCallLogs), "there should be one sudo call to contract")
	require.Equal(t, sudoCallLog{
		ContractAddress: contractAddress1,
		Data:            sudoDataSerialized,
	}, wasmKeeper.SudoCallLogs[0], "sudo call log should have exact parameters")

	require.Equal(t, 0, len(underlyingFeeGrantKeeper.UseGrantedFeeCallLog), "there should be zero call to underlying fee grant keeper")
	require.Equal(t, 0, len(gasTrackerKeeper.TxNonElibigleCallLogs), "there should be zero call to set tx non eligibility")

	accountKeeper.ResetLogs()
	gasTrackerKeeper.ResetLogs()
	wasmKeeper.ResetLogs()

	// Test 7: Everything is alright
	wasmKeeper.FailSudoCall = false
	gasTrackerKeeper.FailTxNonEligibleCall = false
	err = proxyFeeGrantKeeper.UseGrantedFees(sdk.Context{}, accountKeeper.Address, granteeAddress, normalFee, validMsgs)
	require.NoError(t, err, "There should not be an error")
	require.Equal(t, 1, len(accountKeeper.ModuleCallAddressLogs), "there should be one call to get the module address")
	require.Equal(t, gastracker.InflationRewardAccumulator, accountKeeper.ModuleCallAddressLogs[0], "the module name should be of the accumulator")
	require.Equal(t, 1, len(gasTrackerKeeper.GetContractMetadataCallLogs), "there should be one call to get contract metadata")
	require.Equal(t, contractAddress1, gasTrackerKeeper.GetContractMetadataCallLogs[0], "the contract metadata call should be with correct address")
	require.Equal(t, 1, len(gasTrackerKeeper.SetContractMetadataCallLogs), "there should be one call to get contract metadata")
	require.Equal(t, setContractMetadataLog{
		Address: contractAddress1,
		Metadata: gstTypes.ContractInstanceSystemMetadata{
			InflationBalance: normalFeeBalance,
		},
	}, gasTrackerKeeper.SetContractMetadataCallLogs[0], "set contract metadata call should be with correct address and balance")
	require.Equal(t, 1, len(wasmKeeper.SudoCallLogs), "there should be one sudo call to contract")
	require.Equal(t, sudoCallLog{
		ContractAddress: contractAddress1,
		Data:            sudoDataSerialized,
	}, wasmKeeper.SudoCallLogs[0], "sudo call log should have exact parameters")
	require.Equal(t, 1, len(gasTrackerKeeper.TxNonElibigleCallLogs), "there should be one call to set tx non eligibility")
	require.Equal(t, "", gasTrackerKeeper.TxNonElibigleCallLogs[0], "there should be log in place")

	require.Equal(t, 0, len(underlyingFeeGrantKeeper.UseGrantedFeeCallLog), "there should be zero call to underlying fee grant keeper")
}