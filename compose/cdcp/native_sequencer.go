package cdcp

import (
	"github.com/compose-network/specs/compose"
	"github.com/compose-network/specs/compose/scp"
	"github.com/rs/zerolog"
)

// NativeSequencerInstance is an interface that represents the native-sequencer logic for a CDCP instance.
// It has the exact same interface as in SCP.
type NativeSequencerInstance interface {
	scp.SequencerInstance
}

// NewNativeSequencerInstance returns a new native-sequencer CDCP instance.
// It has the exact same behavior as in SCP, and thus its constructor can be used.
func NewNativeSequencerInstance(
	instance compose.Instance,
	execution scp.ExecutionEngine,
	network scp.SequencerNetwork,
	vmSnapshot compose.StateRoot,
	logger zerolog.Logger,
) (NativeSequencerInstance, error) {
	return scp.NewSequencerInstance(instance, execution, network, vmSnapshot, logger)
}
