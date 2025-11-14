# Superblock Construction Protocol v2 (SBCP v2)

This document specifies SBCP v2, which has as objectives:
1. The orchestration of scheduling and execution of composability instances.
2. The progression of the L1 common settlement contract.

To achieve (1), the publisher will have a queue of requests and
will initialize them such that a rollup is never part of two different
instances at the same time.

To achieve (2), the publisher will organize time into *periods*,
such that, once a period ends, it will trigger the settlement pipeline
for proving the activity during the ended period.
Once the pipeline finishes successfully, the publisher will
publish the unique proof to L1, advancing the state.
In case the proof can't be generated within a reasonable time window,
the publisher will trigger a rollback.
Notice that, while the publisher orchestrates the settlement progression
and input, the L1 contract is responsible for the validity and security of
the state updates.

Note that the SCP and settlement protocols will be used as building blocks for the SBCP.

![sbcp](./images/sbcp.png)

> [!TIP]
> If you prefer to first read an informal intuition about the protocol,
> please read the [SBCP v2 Informal Intuition](#informal-intuition) section.

## Table of Contents

- [Comparison to V1](#comparison-to-v1)
- [System Model](#system-model)
- [Properties](#properties)
- [Time And Periods](#time-and-periods)
- [Protocol](#protocol)
  - [Messages](#messages)
  - [Shared Publisher](#shared-publisher)
  - [Sequencer](#sequencer)
- [Informal Intuition](#informal-intuition)

## Comparison to V1

This version intends to solve issues with SBCP v1 in the following ways:
- Remove the SBCP v1 tight "slot" coupling (`StartSlot` and `RequestSeal` messages).
Now, rollups can have independent block times.
- Define a *superblock* concept aligned with settlement.
While settlement was producing a *superblock* every ~1 hour, according to its own rules,
SBCP v1 was using another concept of a *superblock*, which was produced every 12 seconds. 
- Allow parallel composability instances execution for disjoint sets of participating chains,
which was strictly sequential in SBCP v1.
- Align rollback logic to failure of the proof pipeline.
In contrast, V1 could roll back every 12 seconds, due to temporary bad network conditions. 
- Allow rollups to keep sovereignty over the DA layer, instead of making the SP responsible for it.

## System Model

Actors and roles:
- Shared Publisher (SP): the coordinator that schedules composability instance
and triggers period update and the settlement pipeline.
- Native Sequencers: one per rollup, who builds L2 blocks at a self-chosen frequency,
participates in composability when instructed, and produces proofs about its blocks and mailbox activity.

Communication:
- Authenticated, partially synchronous channels between any two actors.

Fault model:
- Crash faults for sequencers, while the SP must be live to guarantee termination. 
- Byzantine misbehavior is mitigated by ZK settlement checks (mailbox consistency, range/aggregation proofs)
and by protocol enforcement (e.g., mailbox contract),
though it's not covered here and should be treated in a later version.

## Properties

**Safety**
- **Agreement**: All correct processes that finalize a superblock number $N$ agree on the same superblock object for $N$, read from L1.
- **Monotonicity**: Finalized superblocks form a single chain, each referencing the previous via parent hash, with monotonically increasing superblock numbers.
- **Composability** Consistency: For every ended period, every pair of chains agree on the same **ordered** set of successful composability instances that both participated. 
- **Sequentiality**: For any rollup, composability instances are executed one at a time (no overlap).

**Liveness** (under partial synchrony and live SP)
- **Superblock Progress**: Eventually, every superblock produced during a period is finalized or discarded and a rollback is triggered. 

## Time And Periods

| Config            | Value                                   |
|-------------------|-----------------------------------------|
| `PERIOD_DURATION` | $10\times 32 \times 12 = 3840$ seconds. |
| `GENESIS_TIME`    | Configurable by implementation.         |


SBCP v2 groups composability instances into long time periods, aligned with the usual settlement cadence.

The default period duration is 10 Ethereum epochs, i.e. 3840 seconds.

Periods are counted from a **genesis** time.
Thus, the period $k$ starts at:

```PeriodStart(k) = GenesisTime + k * PERIOD_DURATION```

Notes:
- Rollups keep independent L2 block times; only period boundaries are common.
- Rollups will be abstracted from the period time logic,
while the SP will trigger them via a start message, as described below.

## Protocol

### Messages

```protobuf
// Internal
message TransactionRequest {
  uint64 chain_id = 1;
  repeated bytes transaction = 2;
}

// User -> Sequencer and Sequencers/Users -> SP
message XTRequest {
  repeated TransactionRequest transaction_requests = 1;
}

// SP -> Sequencers
message StartInstance {
  bytes instance_id = 1;
  uint64 period_id = 2;
  uint64 sequence_number = 3;
  XTRequest xt_request = 4;
}

// SP -> Sequencers
message Decided {
  bytes instance_id = 1;
  bool decision = 2;
}

// SP -> Sequencers
message StartPeriod {
  uint64 period_id = 1;
  uint64 superblock_number = 2;
}

// SP -> Sequencers
message Rollback {
  uint64 period_id = 1;
  uint64 last_finalized_superblock_number = 2;
  bytes last_finalized_superblock_hash = 3;
}

// Sequencers -> SP
message Proof {
  uint64 period_id = 1;
  uint64 superblock_number = 2;
  bytes proof_data = 3;
}
```

> [!Note]
> While `StartInstance` and `Decided` messages are used in the SCP protocol,
> they also appear here as the SBCP's hook to start/finish SCP instances.


### Shared Publisher

The SP is the coordinator of the protocol, keeping track of the current period and
broadcasting an `StartPeriod` message whenever a new period starts.

The `StartPeriod` message carries the new period ID as well as
the number of the superblock number that will be built for the next period.

During a period, the SP will initiate composability instances.
Users' requests are represented by `XTRequest`.
They can either be directly sent to the SP
who will put it in the queue and schedule it,
or the user can send it to a sequencer, who will simply forward it to the SP.

On the one hand, the publisher implementation layer is responsible for managing the queue,
having the flexibility to implement different policies.
On the other hand, the spec will restrict whether an instance can be started or not.
For that, it holds the set of active chains that are currently participating
in a composability instance, updating the set whenever an instance starts or terminates.

For each period, the SP holds a sequence number indicating the order
of a composability instance initiated in the current period.
Upon starting a new period, the sequence number is reset to 0.
The sequence number helps sequencers know the chronological order
of instances and manage messages appropriately, if needed.

Each instance is identified by an `InstanceID` computed as the hash
of the period ID, its sequence number, and the `XTRequest` that triggered it,
i.e. `InstanceID = H(period_id || seq || XTRequest)`.

Once a period finishes, the SP starts the settlement pipeline for
the superblock associated with it,
along with a timer with `ProofWindow` duration.

During the settlement protocol, each rollup will produce a proof and
send it to the SP via the `Proof` message.
Once the SP collects all proofs, it produces the network proof and
publishes it to L1. Once the associated L1 event is received,
the SP updates its settled state.

In case the timer expires and it hasn't yet produced the network proof,
or in case the settlement pipeline fails (e.g., due to invalid proofs),
the SP will broadcast a `Rollback` message
with the last finalized superblock number and hash,
resetting the network to it.

### Sequencer

Whenever a sequencer is participating in a composability instance,
it must not process local transactions nor start other instances.
For that, the sequencer will keep track of a flag to indicate
whether it's currently active on an instance.

The sequencer receives the `StartPeriod` message from the SP, indicating
that a new period has started.
As soon as it closes the last block from the previous period,
it will start the settlement pipeline for it.
Once a proof is generated, the sequencer sends it to the SP.

Once a `Rollback` message is received from the SP,
the sequencer rolls back to the last finalized state
which is updated with L1 events and should coincide with
the message's content.



## Informal Intuition

Our building blocks are:
- SCP: provides a way for sequencers to agree on including or not a request.
- Settlement: allows constructing a valid ZKP to be posted to L1,
given that the chains produced valid L2 blocks and agree on the mailbox state.

Agreeing on the mailbox state means that all rollups started the settlement
with the same set of decided composability instances, in the same order.

**Example**: Suppose all cross-chain requests involve chains A and B.
If chain A executed the requests $(r_1, r_2, r_3)$, then,
in order for the mailbox states to be consistent, chain B
must also have executed the requests $(r_1, r_2, r_3)$ in the same order.
If B started the settlement pipeline with its state updated only by $(r_1, r_2)$
(or in a different order like $(r_2, r_1, r_3)$),
then the consistency check would fail.

That's the goal of the SBCP: to make sure sequencers start the settlement pipeline
at common states.

Besides this, because rollups can't participate in more than one composability instance
at a time (due to EVM sequentiality and instance isolation), SBCP must also enforce that.
This logic should exist in the SP who starts and manages the instances.
