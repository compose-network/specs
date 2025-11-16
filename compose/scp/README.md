# Synchronous Composability Protocol (SCP) — Minimal Spec Implementation

This package provides a minimal,
testable implementation of the [SCP](./../../synchronous_composability_protocol.md)
protocol.

## Publisher

The package provides the `PublisherInstance` interface with the core logic
for the publisher role in SCP.
It requires the following implementation dependency:
- `PublisherNetwork`: to send `StartInstance` and `Decided` messages to all participants.

And provides the following methods:
- `Instance()`: returns the `compose.Instance` metadata (ID, period, sequence, request).
- `DecisionState()`: returns the current decision state (`Pending`, `Accepted`, `Rejected`).
- `Run()`: starts the instance by broadcasting `StartInstance`.
- `ProcessVote(sender, vote)`: processes a vote from a participant chain.
  - Any `false` vote decides the instance as rejected immediately.
  - All `true` votes decide the instance as accepted.
  - Duplicated votes are rejected; non-participant votes are ignored.
- `Timeout()`: decides the instance as rejected if still pending.

```mermaid
classDiagram
  direction TB

  class PublisherInstance {
    +Instance() Instance
    +DecisionState() DecisionState
    +Run()
    +ProcessVote(ChainID, bool) error
    +Timeout() error
  }

  class PublisherNetwork {
    <<interface>>
    +SendStartInstance(Instance)
    +SendDecided(InstanceID, bool)
  }

  class PublisherState {
    instance : Instance
    chains : []ChainID
    decisionState : DecisionState
    votes : map[ChainID]bool
  }

  PublisherInstance --> PublisherNetwork
  PublisherInstance --> PublisherState
```

## Sequencer

The package also provides the `SequencerInstance` interface with the core logic
for the sequencer role in SCP.
It requires the following implementation dependencies:
- `ExecutionEngine`: to simulate transactions with mailbox-aware tracing.
- `SequencerNetwork`: to send mailbox messages to peers and votes to the publisher.

And provides the following methods:
- `DecisionState()`: returns the current decision state.
- `Run()`: starts the instance (upon the `StartInstance` message) and simulates the instance’s local transactions from a VM snapshot.
  - On success (no read miss, no error): sends `Vote(true)` and waits for `Decided`.
  - On read miss: stores the expected header and waits for inbox fulfillment, then re-simulates.
  - On other errors: sends `Vote(false)` and terminates.
- `ProcessMailboxMessage(msg)`: buffers incoming mailbox messages and, when any expected read is fulfilled, re-simulates.
- `ProcessDecidedMessage(decided)`: finalizes the instance as accepted/rejected.
- `Timeout()`: if not already waiting for decision or done, sends `Vote(false)` and terminates.

```mermaid
classDiagram
  direction TB
  
  class SequencerInstance {
    +DecisionState() DecisionState
    +Run() error
    +ProcessMailboxMessage(MailboxMessage) error
    +ProcessDecidedMessage(bool) error
    +Timeout()
  }

  class ExecutionEngine {
    <<interface>>
    +ChainID() ChainID
    +Simulate(SimulationRequest) (*MailboxMessageHeader, []MailboxMessage, error)
  }

  class SequencerNetwork {
    <<interface>>
    +SendMailboxMessage(ChainID, MailboxMessage)
    +SendVote(bool)
  }

  class SequencerState {
    state : SequencerState
    decisionState : DecisionState
    txs : [][]byte
    expectedReadRequests : []MailboxMessageHeader
    pendingMessages : []MailboxMessage
    putInboxMessages : []MailboxMessage
    vmSnapshot : StateRoot
    writtenMessagesCache : []MailboxMessage
  }

  class SimulationRequest {
    PutInboxMessages : []MailboxMessage
    Transactions : [][]byte
    Snapshot : StateRoot
  }

  class MailboxMessageHeader {
    SourceChainID : ChainID
    DestChainID : ChainID
    Sender : EthAddress
    Receiver : EthAddress
    SessionID : SessionID
    Label : string
  }

  class MailboxMessage {
    MailboxMessageHeader
    Data : []byte
  }

  SequencerInstance --> ExecutionEngine
  SequencerInstance --> SequencerNetwork
  SequencerInstance --> SequencerState
  SequencerState --> MailboxMessage
  SequencerState --> MailboxMessageHeader
  ExecutionEngine ..> SimulationRequest
```

Notes:
- The `ExecutionEngine.Simulate` returns at most one read miss header per run; the sequencer loops by re-running after inbox fulfillment.
- `writtenMessagesCache` prevents duplicate mailbox sends when re-simulating.

## Tests

To run the unit tests, use the following command:

```bash
go test ./...
```

## Auxiliary Sequence Flows

### 1. Instance start and initial simulation

```mermaid
sequenceDiagram
  autonumber
  participant SP as Publisher
  participant SA as Sequencer A
  participant EA as ExecutionEngine A

  SP->>SA: StartInstance(id, period, seq, XTRequest)
  note over SA: Stop local txs and locks to a VM snapshot
  SA->>EA: Simulate({putInbox=[], txsA, snapshot})
  EA-->>SA: (readMiss? header) / (writeMessages) / (err?)
  alt success (no read miss, no error)
    SA->>SP: Vote(true)
    note over SA: Wait for Decided
  else read miss
    note over SA: Store expected header and wait inbox
  else error
    SA->>SP: Vote(false)
    note over SA: Terminate
  end
```

### 2. Mailbox exchange and re-simulation

```mermaid
sequenceDiagram
  autonumber
  participant SA as Sequencer A
  participant SB as Sequencer B
  participant EA as ExecutionEngine A
  participant EB as ExecutionEngine B

  note over SA: Has expected read for (B -> A : label)
  SB->>EB: Simulate(...)
  EB-->>SB: writeMessages = [msg(B->A,label,data)]
  SB->>SA: SendMailboxMessage(A, msg)
  SA->>SA: ProcessMailboxMessage(msg)
  SA->>EA: Simulate({putInbox=[msg], txsA, snapshot})
  EA-->>SA: success -> no read miss
  SA->>SP: Vote(true)
```

### 3. Positive decision (all-true votes)

```mermaid
sequenceDiagram
  autonumber
  participant SA as Sequencer A
  participant SB as Sequencer B
  participant SP as Publisher

  SA->>SP: Vote(true)
  SB->>SP: Vote(true)
  SP->>SP: All votes collected -> decide true
  SP-->>SA: Decided(true)
  SP-->>SB: Decided(true)
  SA->>SA: ProcessDecidedMessage(true)
  SB->>SB: ProcessDecidedMessage(true)
```

### 4. Negative decision (error or timeout)

```mermaid
sequenceDiagram
  autonumber
  participant SA as Sequencer A
  participant SP as Publisher

  note over SA: Simulation error OR local timeout
  SA->>SP: Vote(false)
  SP->>SP: Decide false immediately
  SP-->>SA: Decided(false)
  SA->>SA: ProcessDecidedMessage(false)

  %% Publisher-side timer path
  SP->>SP: Timeout() while pending
  SP-->>SA: Decided(false)
```

