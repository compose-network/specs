# Superblock Construction Protocol v2 (SBCP v2) — Minimal Spec Implementation

This package provides a minimal,
testable implementation of the [SBCP v2](./../../superblock_construction_protocol_v2.md)
protocol.

## Publisher

The package provides the `Publisher` interface with the core logic
for the publisher role in SBCP v2.
It requires the following implementation dependencies:
- `PublisherProver`: to request network proofs from collected sequencer proofs.
- `PublisherMessenger`: to broadcast period starts and rollbacks to sequencers.
- `L1`: to publish network proofs to the L1 contract.

And provides the following methods:
- `StartPeriod()`: should be called when a new period starts.
Note that the implementation is responsible for period timers
and for calling this method at the correct times, while the spec performs the transition logic.
- `StartInstance(XTRequest)`: attempts to start a new instance for the given `XTRequest`.
Again, note the implementation is responsible for managing a queue
of pending requests, and it should call the spec function to try to start a new instance.
- `DecideInstance(Instance)`: marks an instance as decided.
- `AdvanceSettledState(SuperblockNumber, SuperBlockHash)`: advances the settled
state whenever an L1 event is received by the implementation.
- `ProofTimeout()`: should be called by the implementation when the proof window expires.
Again, note that the implementation is responsible for the timer management.
- `ReceiveProof(PeriodID, SuperblockNumber, []byte, ChainID)`: called by the implementation
when a sequencer proof is received.

```mermaid
classDiagram
  direction TB

  class Publisher {
    +StartPeriod() error
    +StartInstance(XTRequest) (Instance, error)
    +DecideInstance(Instance) error
    +AdvanceSettledState(SuperblockNumber, SuperBlockHash) error
    +ProofTimeout()
    +ReceiveProof(PeriodID, SuperblockNumber, []byte, ChainID)
  }

  class PublisherState {
    PeriodID : PeriodID
    TargetSuperblockNumber : SuperblockNumber
    LastFinalizedSuperblockNumber : SuperblockNumber
    LastFinalizedSuperblockHash : SuperBlockHash
    Proofs : map[SuperblockNumber]map[ChainID][]byte
    Chains : set[ChainID]
    SequenceNumber : SequenceNumber
    ActiveChains : map[ChainID]bool
    ProofWindow : uint64
  }

  class PublisherProver {
    <<interface>>
    +RequestNetworkProof(SuperblockNumber, SuperBlockHash, [][]byte) ([]byte, error)
  }

  class PublisherMessenger {
    <<interface>>
    +BroadcastStartPeriod(PeriodID, SuperblockNumber)
    +BroadcastRollback(PeriodID, SuperblockNumber, SuperBlockHash)
  }

  class L1 {
    <<interface>>
    +PublishProof(SuperblockNumber, []byte)
  }

  Publisher --> PublisherState
  Publisher --> PublisherProver
  Publisher --> PublisherMessenger
  Publisher --> L1
```

## Sequencer

The package also provides the `Sequencer` interface with the core logic
for the sequencer role in SBCP v2.
It requires the following implementation dependencies:
- `SequencerProver`: to request proofs for sealed blocks.
- `SequencerMessenger`: to forward XTRequests to the publisher and send proofs.

And provides the following methods:
- `StartPeriod(PeriodID, SuperblockNumber)`: called by the implementation
when a `StartPeriod` message is received from the SP.
- `Rollback(SuperblockNumber, SuperBlockHash, PeriodID)`: called by the implementation
when a `Rollback` message is received from the SP.
- `ReceiveXTRequest(XTRequest)`: called by the implementation
when an `XTRequest` is received from a user.
- `AdvanceSettledState(SettledState)`: called by the implementation
whenever an L1 event is received.

Furthermore, it adds a block building policy through the following methods:
- `BeginBlock(BlockNumber)`: should be called by the implementation whenever
it wants to start a new block, returning an error if block creation is not currently allowed (e.g. during an instance).
- `CanIncludeLocalTx()`: should be called by the implementation
to check whether local transactions can be included in the current block.
- `EndBlock(BlockHeader)`: should be called by the implementation
whenever it wants to seal the current block, returning an error if sealing can't be performed at the moment.
- `OnStartInstance(InstanceID, PeriodID, SequenceNumber)`: called by the implementation
when a `StartInstance` message is received from the SP, returning an error if the instance can't be started.
- `OnDecidedInstance(InstanceID)`: called by the implementation
when an instance gets decided, either due to a `Decided` message or due to a local `Vote(0)`.

```mermaid
classDiagram
  direction TB
  
  class Sequencer {
    +StartPeriod(PeriodID, SuperblockNumber) error
    +Rollback(SuperblockNumber, SuperBlockHash, PeriodID) (BlockHeader, error)
    +ReceiveXTRequest(XTRequest)
    +AdvanceSettledState(SettledState)
    +BeginBlock(BlockNumber) error
    +CanIncludeLocalTx() (bool, error)
    +OnStartInstance(InstanceID, PeriodID, SequenceNumber) error
    +OnDecidedInstance(InstanceID) error
    +EndBlock(BlockHeader) error
  }

  class SequencerState {
    PeriodID : PeriodID
    TargetSuperblockNumber : SuperblockNumber
    PendingBlock : *PendingBlock
    ActiveInstanceID : *InstanceID
    LastSequenceNumber : *SequenceNumber
    Head : BlockNumber
    SealedBlockHead : map[PeriodID]SealedBlockHeader
    SettledState : SettledState
  }

  class SequencerProver {
    <<interface>>
    +RequestProofs(*BlockHeader, SuperblockNumber) []byte
  }

  class SequencerMessenger {
    <<interface>>
    +ForwardRequest(XTRequest)
    +SendProof(PeriodID, SuperblockNumber, []byte)
  }

  class PendingBlock {
    Number : BlockNumber
    PeriodID : PeriodID
    SuperblockNumber : SuperblockNumber
  }

  class BlockHeader {
    Number : BlockNumber
    BlockHash : BlockHash
    StateRoot : StateRoot
  }

  class SealedBlockHeader {
    BlockHeader : BlockHeader
    PeriodID : PeriodID
    SuperblockNumber : SuperblockNumber
  }

  class SettledState {
    BlockHeader : BlockHeader
    SuperblockNumber : SuperblockNumber
    SuperblockHash : SuperBlockHash
  }
  
  Sequencer --> SequencerState
  Sequencer --> SequencerProver
  Sequencer --> SequencerMessenger
  SequencerState --> PendingBlock
  SequencerState --> SettledState
  SequencerState --> SealedBlockHeader
```

## Tests

To run the unit tests, use the following command:

```bash
go test ./...
```


## Auxiliary Sequence Flows

### 1. Period start and sequencer settlement trigger

```mermaid
sequenceDiagram
  autonumber
  participant SP as Publisher (SP)
  participant S as Sequencer
  participant P as SequencerProver

  SP->>S: StartPeriod(period_id, target_superblock_number)
  Note over S: Update PeriodID and TargetSuperblockNumber
  S->>S: BeginBlock/EndBlock until last block of previous period sealed
  S->>P: RequestProofs(prevPeriodHead?, target_superblock_number-1)
  P-->>S: proof
  S->>SP: SendProof(prev_period_id, target_superblock_number-1, proof)
```

### 2. XTRequest forwarding and instance start/decision

```mermaid
sequenceDiagram
  autonumber
  participant U as User
  participant S as Sequencer
  participant SP as Publisher (SP)

  U->>S: XTRequest
  S->>SP: ForwardRequest(XTRequest)
  Note over SP: Queue + can_start_instance(policy)
  SP->>SP: StartInstance(XTRequest) -> Instance(period, seq, id)
  SP-->>S: StartInstance(id, period, seq, XTRequest) [SCP]
  S->>S: OnStartInstance(id, period, seq)
  Note over S: Lock local txs
  SP-->>S: Decided(id, decision) [SCP]
  S->>S: OnDecidedInstance(id)
  Note over S: Unlock local txs
```

### 3. Proof collection, network proof, and L1 publish


```mermaid
sequenceDiagram
  autonumber
  participant S1 as Sequencer A
  participant S2 as Sequencer B
  participant SP as Publisher (SP)
  participant NP as PublisherProver
  participant L1 as L1 Contract

  S1->>SP: Proof(period_x, S, proofA)
  S2->>SP: Proof(period_x, S, proofB)
  Note over SP: Wait until proofs from all chains
  SP->>NP: RequestNetworkProof(S, lastFinalizedHash, [proofA, proofB])
  NP-->>SP: networkProof
  SP->>L1: PublishProof(S, networkProof)
  L1-->>SP: Finalized(S, hash)
  SP->>SP: AdvanceSettledState(S, hash)

  L1-->>S2: Finalized(S, hash)
  S2->>S2: AdvanceSettledState(S, hash)
  L1-->>S1: Finalized(S, hash)
  S1->>S1: AdvanceSettledState(S, hash)
```

### 4. Rollback

```mermaid
sequenceDiagram
  autonumber
  participant SP as Publisher (SP)
  participant S as Sequencer

  SP->>SP: ProofTimeout() or pipeline failure
  SP->>S: BroadcastRollback(period_id, lastFinalizedNumber, lastFinalizedHash)
  S->>S: Rollback(lastFinalizedNumber, lastFinalizedHash, currentPeriodID)
  Note over S: Drop unfinalized, reset head/period/targetSuperblock
```
