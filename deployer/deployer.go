package deployer

import (
	"math/big"

	"github.com/SBit-Project/solar/contract"
)

type Options struct {
	AsLib     bool
	Name      string
	Overwrite bool

	// GasPrice is specified in satoshi (sbit) or wei (ethereum)
	GasPrice *big.Float
	GasLimit uint
}

type Deployer interface {
	// FIXME better interface for call options
	CreateContract(contract *contract.CompiledContract, jsonParams []byte, opts *Options) error
	ConfirmContract(contract *contract.DeployedContract) error
	Mine() error
}
