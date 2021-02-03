package utxo

import (
	"errors"
	"fmt"

	"github.com/iotaledger/hive.go/kvstore"
	"github.com/iotaledger/hive.go/marshalutil"
	iotago "github.com/iotaledger/iota.go/v2"
)

const (
	// A prefix which denotes a spent treasury output.
	TreasuryOutputSpentPrefix = 1
	// A prefix which denotes an unspent treasury output.
	TreasuryOutputUnspentPrefix = 0
)

var (
	// Returned when the state of the treasury is invalid.
	ErrInvalidTreasuryState = errors.New("invalid treasury state")
)

// TreasuryOutput represents the output of a treasury transaction.
type TreasuryOutput struct {
	// The ID of the milestone which generated this output.
	MilestoneID iotago.MilestoneID
	// The amount residing on this output.
	Amount uint64
	// Whether this output was already spent
	Spent bool
}

func (t *TreasuryOutput) kvStorableKey() (key []byte) {
	return marshalutil.New(34).
		WriteByte(UTXOStoreKeyPrefixTreasuryOutput).
		WriteBool(t.Spent).
		WriteBytes(t.MilestoneID[:]).
		Bytes()
}

func (t *TreasuryOutput) kvStorableValue() (value []byte) {
	return marshalutil.New(8).
		WriteUint64(t.Amount).
		Bytes()
}

func (t *TreasuryOutput) kvStorableLoad(_ *Manager, key []byte, value []byte) error {
	keyExt := marshalutil.New(key)
	// skip prefix
	if _, err := keyExt.ReadByte(); err != nil {
		return err
	}

	spent, err := keyExt.ReadBool()
	if err != nil {
		return err
	}

	milestoneID, err := keyExt.ReadBytes(iotago.MilestoneIDLength)
	if err != nil {
		return err
	}
	copy(t.MilestoneID[:], milestoneID)

	val := marshalutil.New(value)
	t.Amount, err = val.ReadUint64()
	if err != nil {
		return err
	}

	t.Spent = spent

	return nil
}

// stores the given treasury output.
func storeTreasuryOutput(output *TreasuryOutput, mutations kvstore.BatchedMutations) error {
	return mutations.Set(output.kvStorableKey(), output.kvStorableValue())
}

// deletes the given treasury output.
func deleteTreasuryOutput(output *TreasuryOutput, mutations kvstore.BatchedMutations) error {
	return mutations.Delete(output.kvStorableKey())
}

// marks the given treasury output as spent.
func markTreasuryOutputAsSpent(output *TreasuryOutput, mutations kvstore.BatchedMutations) error {
	outputCopy := *output
	outputCopy.Spent = false
	if err := mutations.Delete(outputCopy.kvStorableKey()); err != nil {
		return err
	}
	outputCopy.Spent = true
	return mutations.Set(outputCopy.kvStorableKey(), outputCopy.kvStorableValue())
}

// marks the given treasury output as unspent.
func markTreasuryOutputAsUnspent(output *TreasuryOutput, mutations kvstore.BatchedMutations) error {
	outputCopy := *output
	outputCopy.Spent = true
	if err := mutations.Delete(outputCopy.kvStorableKey()); err != nil {
		return err
	}
	outputCopy.Spent = false
	return mutations.Set(outputCopy.kvStorableKey(), outputCopy.kvStorableValue())
}

func (u *Manager) readSpentTreasuryOutputWithoutLocking(msHash []byte) (*TreasuryOutput, error) {
	key := append([]byte{UTXOStoreKeyPrefixTreasuryOutput, TreasuryOutputSpentPrefix}, msHash...)
	val, err := u.utxoStorage.Get(key)
	if err != nil {
		return nil, err
	}
	to := &TreasuryOutput{}
	if err := to.kvStorableLoad(nil, key, val); err != nil {
		return nil, err
	}
	return to, nil
}

func (u *Manager) readUnspentTreasuryOutputWithoutLocking(msHash []byte) (*TreasuryOutput, error) {
	key := append([]byte{UTXOStoreKeyPrefixTreasuryOutput, TreasuryOutputUnspentPrefix}, msHash...)
	val, err := u.utxoStorage.Get(key)
	if err != nil {
		return nil, err
	}
	to := &TreasuryOutput{}
	if err := to.kvStorableLoad(nil, key, val); err != nil {
		return nil, err
	}
	return to, nil
}

// AddTreasuryOutput adds the given treasury output to the database.
func (u *Manager) AddTreasuryOutput(to *TreasuryOutput) error {
	return u.utxoStorage.Set(to.kvStorableKey(), to.kvStorableValue())
}

// DeleteTreasuryOutput deletes the given treasury output from the database.
func (u *Manager) DeleteTreasuryOutput(to *TreasuryOutput) error {
	return u.utxoStorage.Delete(to.kvStorableKey())
}

// Returns the unspent treasury output.
func (u *Manager) UnspentTreasuryOutput() (*TreasuryOutput, error) {
	var i int
	var innerErr error
	var unspentTreasuryOutput *TreasuryOutput
	if err := u.utxoStorage.Iterate([]byte{UTXOStoreKeyPrefixTreasuryOutput, TreasuryOutputUnspentPrefix}, func(key kvstore.Key, value kvstore.Value) bool {
		i++
		unspentTreasuryOutput = &TreasuryOutput{}
		if err := unspentTreasuryOutput.kvStorableLoad(u, key, value); err != nil {
			innerErr = err
			return false
		}
		return true
	}); err != nil {
		return nil, err
	}

	if innerErr != nil {
		return nil, innerErr
	}

	switch {
	case i > 1:
		return nil, fmt.Errorf("%w: more than one unspent treasury output exists", ErrInvalidTreasuryState)
	case i == 0:
		return nil, fmt.Errorf("%w: no treasury output exists", ErrInvalidTreasuryState)
	}

	return unspentTreasuryOutput, nil
}

type TreasuryOutputConsumer func(output *TreasuryOutput) bool

func (u *Manager) ForEachTreasuryOutput(consumer TreasuryOutputConsumer, options ...UTXOIterateOption) error {

	opt := iterateOptions(options)

	if opt.readLockLedger {
		u.ReadLockLedger()
		defer u.ReadUnlockLedger()
	}

	var innerErr error
	var i int
	if err := u.utxoStorage.Iterate([]byte{UTXOStoreKeyPrefixTreasuryOutput}, func(key kvstore.Key, value kvstore.Value) bool {

		if (opt.maxResultCount > 0) && (i >= opt.maxResultCount) {
			return false
		}

		i++

		output := &TreasuryOutput{}
		if err := output.kvStorableLoad(u, key, value); err != nil {
			innerErr = err
			return false
		}

		return consumer(output)
	}); err != nil {
		return err
	}

	return innerErr
}

func (u *Manager) ForEachSpentTreasuryOutput(consumer TreasuryOutputConsumer, options ...UTXOIterateOption) error {

	opt := iterateOptions(options)

	if opt.readLockLedger {
		u.ReadLockLedger()
		defer u.ReadUnlockLedger()
	}

	var innerErr error
	var i int
	if err := u.utxoStorage.Iterate([]byte{UTXOStoreKeyPrefixTreasuryOutput, TreasuryOutputSpentPrefix}, func(key kvstore.Key, value kvstore.Value) bool {

		if (opt.maxResultCount > 0) && (i >= opt.maxResultCount) {
			return false
		}

		i++

		output := &TreasuryOutput{}
		if err := output.kvStorableLoad(u, key, value); err != nil {
			innerErr = err
			return false
		}

		return consumer(output)
	}); err != nil {
		return err
	}

	return innerErr
}
