# Cross-Domain Composability Protocol (CDCP) — Minimal Spec Implementation

This package provides a minimal, testable implementation of the [CDCP](./../../cross_domain_composability_protocol.md) protocol that coordinates atomic execution between Compose native rollups and an external rollup.

## Publisher

The package exposes the `PublisherInstance` interface with the core logic for the CDCP publisher role.
It requires the following implementation dependency:
- `PublisherNetwork`: transports `StartInstance` to all participants,
`NativeDecided` to the WS, and final `Decided` messages to all NSs.

And provides the following methods:
- `Instance()`: returns the immutable `compose.Instance` metadata (ID, period, sequence number, chain list, request payload).
- `DecisionState()`: returns the current decision state (`Pending`, `Accepted`, `Rejected`).
- `Run()`: broadcasts the `StartInstance` message to every participant chain.
- `ProcessVote(sender, vote)`: records votes from native chains only, rejecting duplicates and ignoring non-native senders. Any `false` vote immediately decides and broadcasts rejection while `true` votes from all natives trigger `NativeDecided(true)` to the WS.
- `ProcessWSDecided(sender, decision)`: accepts exactly one decision from the ER chain. `false` finalizes the instance immediately (even if native votes are still missing); `true` is only valid after `NativeDecided(true)` has been sent and leads to acceptance.
- `Timeout()`: if still waiting for native votes, decides rejection and emits both `Decided(false)` and `NativeDecided(false)`.

The publisher validates the instance at construction time: the ER chain must belong to the instance and there must be at least one native chain.
Its state machine advances from WaitingVotes → WaitingWSDecided → Done, guarding impossible combinations (e.g. `WSDecided(true)` while still waiting for votes).

```mermaid
classDiagram
  direction TB

  class PublisherInstance {
    +Instance() Instance
    +DecisionState() DecisionState
    +Run()
    +ProcessVote(ChainID, bool) error
    +ProcessWSDecided(ChainID, bool) error
    +Timeout() error
  }

  class PublisherNetwork {
    <<interface>>
    +SendStartInstance(Instance)
    +SendNativeDecided(InstanceID, bool)
    +SendDecided(InstanceID, bool)
  }

  class PublisherState {
    instance : Instance
    nativeChains : map[ChainID]struct
    erChainID : ChainID
    state : PublisherState
    decisionState : DecisionState
    votes : map[ChainID]bool
    wsDecision : *bool
  }

  PublisherInstance --> PublisherNetwork
  PublisherInstance --> PublisherState
```

## Native Sequencer

Native sequencer behavior in CDCP is identical to the SCP sequencer role.
`NewNativeSequencerInstance` simply proxies to `scp.NewSequencerInstance`, so implementers can reuse the same execution engine, network adapter, and VM snapshot logic described in [`scp`](./../scp/README.md).

## Wrapped Sequencer

Wrapped sequencer (WS) logic is provided through the `WrappedSequencerInstance` interface.
It orchestrates mailbox-aware simulations of the external rollup transactions, waits for the native decision, and interacts with the external rollup client to submit the canonical transaction.
It requires the following implementation dependencies:
- `WSExecutionEngine`: performs mailbox-aware simulations of `safe_execute`, returning read/write misses plus the produced mailbox messages.
- `WSNetwork`: sends mailbox messages to native chains and reports `WSDecided` results back to the publisher.
- `ERClient`: submits `safe_execute` on the external rollup once the native decision allows it.

And provides the following methods:
- `DecisionState()`: returns the WS decision state.
- `Run()`: calls simulations against the VM snapshot:
  - Success: caches written mailbox messages, transitions to `WaitingNativeDecided`, and waits for publisher's `NativeDecided` message.
  - Error: transitions to `Done` and reports the error.
- `ProcessMailboxMessage(msg)`: buffers incoming mailbox messages for simulation; when a pending read is satisfied it moves the message into `PutInboxMessages` and re-runs the simulation.
- `ProcessNativeDecidedMessage(decided)`: deduplicates the publisher’s native decision and, if `true`, triggers the external rollup submission; `false` cancels execution.
- `Timeout()`: while not already waiting for the ER response, aborts and reports `WSDecided(false)`.

```mermaid
classDiagram
  direction TB

  class WrappedSequencerInstance {
    +DecisionState() DecisionState
    +Run() error
    +ProcessMailboxMessage(MailboxMessage) error
    +ProcessNativeDecidedMessage(bool) error
    +Timeout()
  }

  class WSExecutionEngine {
    <<interface>>
    +ChainID() ChainID
    +Simulate(WSSimulationRequest) WSSimulationResponse
  }

  class ERClient {
    <<interface>>
    +SubmitTransaction(SafeExecuteArguments) error
  }

  class WSNetwork {
    <<interface>>
    +SendMailboxMessage(ChainID, MailboxMessage)
    +SendWSDecidedMessage(bool)
  }

  class SafeExecuteArguments {
    PutInboxMessages : []MailboxMessage
    PutOutboxMessages : []MailboxMessage
    Transactions : [][]byte
  }

  class WSSimulationRequest {
    SafeExecuteArguments
    Snapshot : StateRoot
  }

  class WSSimulationResponse {
    ReadMiss : *MailboxMessageHeader
    WriteMiss : *MailboxMessage
    WrittenMessages : []MailboxMessage
    Err : error
  }

  class WSStateData {
    state : WSState
    decisionState : DecisionState
    nativeDecided : *bool
    txs : [][]byte
    expectedReadRequests : []MailboxMessageHeader
    pendingMessages : []MailboxMessage
    putInboxMessages : []MailboxMessage
    writePrePopulationMessages : []MailboxMessage
    writtenMessagesCache : []MailboxMessage
    vmSnapshot : StateRoot
  }

  WrappedSequencerInstance --> WSExecutionEngine
  WrappedSequencerInstance --> WSNetwork
  WrappedSequencerInstance --> ERClient
  WrappedSequencerInstance --> WSStateData
  WSSimulationRequest --> SafeExecuteArguments
  WSSimulationResponse --> MailboxMessage
```

## Tests

To run the unit tests, use:

```bash
go test ./...
```

## Auxiliary Sequence Flows

### 1. Instance start, native votes, and WS decision

```mermaid
sequenceDiagram
  autonumber
  participant SP as Publisher
  participant NS as Native Sequencer
  participant WS as Wrapped Sequencer

  SP->>NS: StartInstance(id, period, seq, XTRequest)
  SP->>WS: StartInstance(id, period, seq, XTRequest)
  NS->>SP: Vote(true/false)
  alt vote == false
    SP->>SP: Decide(false)
    SP-->>NS: Decided(false)
    SP-->>WS: NativeDecided(false)
  else all votes true
    SP-->>WS: NativeDecided(true)
    WS->>SP: WSDecided(true/false)
    SP-->>NS: Decided(decision)
  end
```

### 2. WS simulation, mailbox exchange, and re-run

```mermaid
sequenceDiagram
  autonumber
  participant WS as Wrapped Sequencer
  participant EE as WSExecutionEngine
  participant NS as Native Sequencer

  WS->>EE: Simulate({putInbox, putOutbox, txs, snapshot})
  EE-->>WS: ReadMiss(header)
  WS->>WS: Store expected read
  NS-->>WS: MailboxMessage(header, data)
  WS->>WS: consumeReceivedMailboxMessages
  WS->>EE: Simulate({putInbox+msg, putOutbox, txs, snapshot})
  EE-->>WS: (success + writtenMessages)
  WS->>NS: SendMailboxMessage(dest, msg)
```

### 3. Native decision, ER submission, and finalization

```mermaid
sequenceDiagram
  autonumber
  participant WS as Wrapped Sequencer
  participant ER as ERClient
  participant SP as Publisher

  WS->>SP: (waiting) NativeDecided?
  SP-->>WS: NativeDecided(true)
  WS->>ER: SubmitTransaction({putInbox, putOutbox, txs})
  alt success
    ER-->>WS: ok
    WS->>SP: WSDecided(true)
    SP-->>NSs: Decided(true)
  else failure or timeout
    ER-->>WS: error
    WS->>SP: WSDecided(false)
    SP-->>NSs: Decided(false)
  end
```
