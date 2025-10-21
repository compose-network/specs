# Superblock Construction Protocol v2 (SBCP v2) <!-- omit from toc -->

This document specifies SBCP v2: the orchestration layer that schedules and constrains execution of SCP/CDCP instances, and governs sealing, settlement alignment, and rollback.
It removes the slot requirement from SBCP v1 and relies on a superblock time period, aligned with the settlement layer’s batching horizon.

SBCP v2 provides:
- A timebox (superblock period) within which SCP/CDCP instances are started and decided.
- A sequential execution policy for instances that share any rollup, while permitting concurrency for disjoint sequencers sets.
- Deterministic sealing and handoff to the settlement layer.
- A rollback model bounded to superblock boundaries.


## Table of Contents <!-- omit from toc -->

- [Objectives](#objectives)
- [System Model](#system-model)
- [Properties](#properties)
- [Time And Periods](#time-and-periods)
- [Identifiers And Counters](#identifiers-and-counters)
- [Protocol Overview](#protocol-overview)
    - [Messages](#messages)
    - [Shared Publisher](#shared-publisher)
    - [Sequencers](#sequencers)
    - [Scheduling And Parallelism Policy](#scheduling-and-parallelism-policy)


## Objectives

- Remove the SBCP v1 “slot” coupling: rollups keep independent block times.
- Define a superblock period aligned with settlement batching (e.g., range/aggregation cadence) and treat composability consistency only at period boundaries.
- Specify how SCP and CDCP instances are initiated, ordered, and terminated within a period.
- Enforce sequential execution for instances that share a sequencer, and allow parallelism when participant sets are disjoint.
- Bound rollbacks to superblock boundaries.


## System Model

Actors and roles:
- Shared Publisher (SP): single coordinator that schedules SCP/CDCP instances, enforces sequencing and cutovers, and drives sealing. Assumed crash-stop for liveness (i.e., cannot remain crashed indefinitely).
- Native Sequencers: one per native rollup, build L2 blocks at self-chosen cadence; participate in SCP/CDCP when instructed.
- Wrapped Sequencer (WS): represents an external rollup and participates in CDCP instances.

Communication:
- Authenticated, partially synchronous channels between SP and sequencers/WS, and among sequencers for mailbox messages as per SCP/CDCP.

Timing and clocks:
- No global L2 slot or block time, though the SP uses an L1-aligned clock to define the superblock period and sealing cutover windows.

Fault model:
- Crash faults for sequencers. SP must be live to guarantee progress.
- Byzantine misbehavior is mitigated by ZK settlement checks (mailbox consistency, range/aggregation proofs) and by protocol enforcement (e.g., mailboxes contracts), though it should be encompassed in a later version.


## Properties

**Safety**
- Agreement (Superblock): All correct processes that finalize period P agree on the same `SuperblockBatch` for P.
- Monotonicity: Finalized superblocks form a single chain; each references the previous via parent hash.
- Mailbox Coherence: At every finalized period boundary, for each pair of chains (i, j), `inbox_i[j] == outbox_j[i]` (proved at settlement, but guaranteed by SBCP scheduling and superblock periods).
- Sequentiality: For any rollup `r`, SCP/CDCP instances that include `r` are executed one at a time (no overlap).

**Liveness** (under partial synchrony and live SP)
- Period Progress: If every required range/aggregation proof for period P is eventually produced, SP seals and submits a network proof, and P finalizes.
- Instance Termination: Every started SCP/CDCP instance eventually decides (1 or 0) given bounded timeouts.

**Rollback bounds**
- Rollbacks only occur at superblock boundaries; no on-chain finality is exposed mid-period for the superblock state.


## Time And Periods

| Config                         | Value |
|--------------------------------| --- |
| `EPOCHS_PER_SUPERBLOCK_PERIOD` | 10 |

SBCP v2 fixes the superblock period to 10 Ethereum Beacon epochs, i.e. $10 \cdot (32 \cdot 12) = 3,840$ seconds.

For computing the current period,
let `E(0)` be the genesis L1 epoch anchor for SBCP v2 (recorded in config or on-chain),
and `E(t)` be the current Beacon epoch observed by SP at time t.
Then, the period is computed as:

```period_id(t) = 1 + floor((E(t) - E(0)) / EPOCHS_PER_SUPERBLOCK_PERIOD)```

For a period `P`, the boundary epochs are:

```open_epoch(P)  = E(0) + EPOCHS_PER_SUPERBLOCK_PERIOD · (P − 1)```
```close_epoch(P) = E(0) + (EPOCHS_PER_SUPERBLOCK_PERIOD · P) − 1```

Notes:
- Rollups keep independent L2 block times; only period boundaries are common.
- The settlement layer aggregates per-rollup batches over exactly these 10 epochs and produces one network proof per period.


## Identifiers And Counters

SBCP v2 uses two distinct notions that must not be conflated:

- Execution period identifier: `period_id`
    - Derived from L1 Beacon epochs as specified above.
    - Drives scheduling (StartPeriod) and tags SCP/CDCP instances with `(period_id, seq)`.
    - Advances deterministically with epochs, independent of settlement.

- On-chain finalization counter: `last_finalized_superblock`
    - Monotonically increases only when the L1 contract accepts and finalizes the proof for the next `SuperblockBatch`.
    - May lag behind `period_id` due to proving latency or bad periods.

Invariants
- `last_finalized_superblock` increases by 1 per finalized submission and never exceeds the largest sealed period.
- Rollback, when required, targets `last_finalized_superblock` (and its hash) and invalidates any sealed-but-unfinalized later periods.
- The SP enforces a bounded proving window `PROOF_WINDOW`. If the proof for `last_finalized_superblock + 1` is not accepted within this window from the period boundary, a rollback to `last_finalized_superblock` is triggered.

Different modes
- On a fully happy path scenario, every period finalizes, and so `last_finalized_superblock` always equals the current `period_id` minus 1.
- On rollbacks, the `last_finalized_superblock` stays stuck while `period_id` advances. More clearly, note that, once a superblock is posted, it satisfies `superblockNumber = last_finalized_superblock + 1` (and not `= current period_id`).


## Protocol Overview

Within each period `P`:
1. SP accepts user requests and enqueues SCP/CDCP instances.
2. SP schedules instances, enforcing sequentiality per rollup and allowing concurrency only across disjoint rollups sets.
3. Near the end of `P`, SP enters a sealing cutover (internal logic): stop starting new instances and let ongoing instances terminate.
4. Once `P` ends, SP forcefully closes (if possible) instances and sends a `StartPeriod(P+1)` message to all sequencers, indicating the beginning of the new active period and the end of `P`.
5. Sequencers independently complete their local L2 blocks for `P` and start the settlement pipeline, which terminates with the SP publishing to L1.
6. If settlement fails, the SP rolls back to the last finalized superblock and restarts the current period construction on top of the last proved state.


### Messages

```protobuf
message StartPeriod {
  uint64 period_id = 1;                  // New active period
  uint64 target_superblock_number = 2;   // Next superblock number to produce
}

message RollBackTo {
  uint64 last_finalized_superblock_number = 1;    // Last proven superblock number
  bytes32 last_finalized_superblock_hash = 2;     // Hash of SuperblockBatch
  uint64 period_id = 3;                           // Current active period
}
```

Message binding to counters
- SCP's and CDCP's protocol messages MUST be tagged with `(period_id, seq)` to identify the instance within the current period.
- Sequencers MUST reject (vote 0) if `period_id` differs from the locally active period.


### Shared Publisher

The SP always opens the next period at the boundary regardless of settlement status.
Sealing and settlement for the previous period run in parallel and do not block scheduling in the new period.

**State**
```
# Finalized
last_finalized_superblock_number: uint64
last_finalized_superblock_hash: bytes32

# Queue with pending requests
queue: PriorityQueue<xTRequest>             # global across periods

# Current period
current_period_id: uint64                    # current period_id
current_target_superblock_number: uint64     # current target superblock number
active: Set<Instance>                        # SCP/CDCP instances started in the current period
sequence_number: uint64                      # per-period sequence counter (monotone)
sched_index: Map<ChainID, sequence_number>   # last seq seen per chain (sequentiality constraint)
cutover_time: timestamp                      # internal cutover time for the current period 
settlement_deadline_time: timestamp          # time by which proof must be accepted (boundary + PROOF_WINDOW)

# CDCP spanning and blocking
blocked: <InstanceID, Set<ChainID>>          # Instance waiting for WSDecided and blocked chains

# Config
CUTOVER_DURATION = 10 seconds                # time before period ends, to stop scheduling new instances
PROOF_WINDOW = (2/3) · PERIOD_DURATION    # allowed proving time since boundary (default: two-thirds of period)
```

Transitions and triggers
- On boundary: Broadcast `StartPeriod(period_id, target_superblock_number)`, reset `sequence_number`, clear `sched_index`, and sets an internal cutover time for this period. If instances for the previous period still exist, try finalizing as below:
    - An SCP instance is forcefully closed by sending `Decided(0)`.
    - An CDCP instance can only be forcefully closed if `NativeDecided` wasn't yet sent. If so, it can be closed by sending `Decided(0)` and `NativeDecided(0)`; otherwise, it must wait for the WS decision.
    - Still, the new period starts with the associated rollups being blocked (`blocked`) from participating in new instances until the CDCP instance terminates.
  Also set a proving deadline aligned to `PROOF_WINDOW` for the superblock `S = last_finalized_superblock_number + 1` (from the previous period):
    - `settlement_deadline_time = time_now() + PROOF_WINDOW` (default: two-thirds of the period after the boundary).
- While before cutover: When `queue != empty`, try to schedule the head request (see Scheduling). If admissible, emit `StartSC`/`StartCDCP(period_id, seq=sequence_number++)` and record in `active`.
- Once an instance finishes, remove it from the active set.
- At cutover (internal): Stop starting new instances for this period.
- Settlement success event (async): On L1 acceptance of a proof for a new higher superblock number `X`, set `last_finalized_superblock_number = X` and store its hash.
- Settlement failure event (async): Emit `RollBackTo(last_finalized_superblock_number, last_finalized_superblock_hash)`, which invalidates any sealed but unfinalized later periods, and restarts the current period on top of the last proved state.
- Proof deadline: If `time_now() > settlement_deadline_time` and `last_finalized_superblock_number != current_target_superblock_number-1`, treat it as a settlement failure and trigger `RollBackTo(last_finalized_superblock_number, last_finalized_superblock_hash)`.

The cutover time should be such that instances have enough time to terminate before the next period.
For that, we set `CUTOVER_DURATION = 10 seconds`.
`PROOF_WINDOW` defines a bounded proving window (by default, two-thirds of a period). Concretely, at the boundary to a period, the SP sets `settlement_deadline_time = time_now() + PROOF_WINDOW`. If not finalized by then, a rollback is executed.

Pseudo-code (succinct)
```
on_boundary(new_period_id):
  current_period_id = new_period_id
  current_target_superblock_number = last_finalized_superblock_number + 1
  broadcast StartPeriod{period_id: current_period_id, target_superblock_number: current_target_superblock_number}
  sequence_number = 0
  sched_index.clear()
  cutover_time = time_now() + (PERIOD_DURATION - CUTOVER_DURATION)
  settlement_deadline_time = time_now() + PROOF_WINDOW
  for inst in active:
    if inst.type == SCP:
      finalize(inst, 0)  # send Decided(0)
    else if inst.type == CDCP:
      if inst.state == WAIT_WS:
        blocked = <inst.id, Participants(inst)>
      else:
        finalize(inst, 0)  # send Decided(0) and NativeDecided(0)

on_instance_terminated(inst):
  if blocked.instance.id == inst.id:
    blocked <- ⊥
  else:
    active.remove(inst)

try_schedule():
  while now() < cutover_time and not queue.empty():
    req = queue.peek()
    if conflicts_with_active(req) or touches_blocked_chains(req): break
    queue.pop()
    inst = new_instance(req, current_period_id, sequence_number++)
    active.add(inst)
    if inst.type == CDCP: emit StartCDCP(inst)
    else: emit StartSC(inst)

on_timer_tick(now_ts):
  # Deadline-triggered rollback if proof not accepted in time
  if now_ts > settlement_deadline_time and last_finalized_superblock_number != current_target_superblock_number - 1:
    emit RollBackTo{last_finalized_superblock_number, last_finalized_superblock_hash}
```

### Sequencers

**State**
```
active_period_id: uint64
active_target_superblock_number: uint64     # from StartPeriod.target_superblock_number
ongoing_instance: Optional<InstanceID>      # at most one SCP/CDCP instance at a time
blocks: Map<BlockNumber, { block, period_id, sb_target }>
last_proved_state: {
  period_id: uint64,
  superblock_number: uint64,
  l2_head_hash: bytes32,
  l2_head_number: uint64,
  l2_state_root: bytes32
}
settlement_deferred: bool                   # StartPeriod received while instance ongoing
pending_settlement_period: Optional<uint64> # previous period to settle
```

Rules
- Tagging: Every constructed L2 block is tagged with the current `active_period_id` and the current `active_target_superblock_number` as `sb_target`, and stored in `blocks`.
- Single-instance: The sequencer participates in at most one SCP/CDCP instance at a time. While ongoing, local txs that could interfere are paused until decision/timeout.
- Settlement triggers:
    - If `StartPeriod(new_id)` is received and `ongoing_instance == ⊥`, immediately start the settlement pipeline for `pending_settlement_period = new_id − 1`.
    - If `StartPeriod(new_id)` is received and `ongoing_instance != ⊥`, set `settlement_deferred = true`, `pending_settlement_period = new_id − 1`, and start settlement right after the instance terminates.
- Rollback: Upon `RollBackTo(S, H(S))`, discard blocks with `sb_target > S` and restore chain head to `last_proved_state` (which must correspond to S).
- Update proof anchor: On settlement success for period P (finalizing superblock S), update `last_proved_state` with the chain-specific head committed in SuperblockBatch(S).

Pseudo-code (succinct)
```
on_StartPeriod(new_id, target_superblock_number):
  pending_settlement_period = (new_id - 1)
  active_period_id = new_id
  active_target_superblock_number = target_superblock_number
  if ongoing_instance == ⊥:
    start_settlement_pipeline(pending_settlement_period)
  else:
    settlement_deferred = true

on_StartSC_or_StartCDCP(inst):
  if inst.period_id != active_period_id: reject
  ongoing_instance = inst.id

on_Decided(inst):
  if ongoing_instance == inst.id:
    ongoing_instance = ⊥
    if settlement_deferred:
      settlement_deferred = false
      start_settlement_pipeline(pending_settlement_period)

on_new_local_block(b):
  b.period_id = active_period_id
  b.sb_target = active_target_superblock_number
  blocks[b.number] = {block: b, period_id: active_period_id, sb_target: active_target_superblock_number}

start_settlement_pipeline(P):
  # Select last block tagged with period_id == P (if any)
  blks = blocks_with_period(P)
  if blks == ⊥: return  # chain may have been idle in P
  produce_range_and_agg_outputs(blks)
  send_aggregation_outputs_to_SP(P)

on_settlement_success(P, S, chain_head_hash, chain_head_number, post_state_root):
  last_proved_state = {period_id: P, superblock_number: S, l2_head_hash: chain_head_hash, l2_head_number: chain_head_number, l2_state_root: post_state_root}

on_RollBackTo(S, sb_hash):
  # Discard blocks produced for superblocks newer than S
  drop_all_blocks_with_sb_target_gt(S)
  restore_chain_head(last_proved_state)
```


### Scheduling And Parallelism Policy

Definitions
- Participants(xT): the set of chains (native or wrapped) that the instance touches.
- Conflict(x, y): `Participants(x) ∩ Participants(y) ≠ ∅`.

Policy
- Sequentiality: If an instance `y` conflicts with an active instance `x`, `y` must wait.
- Disjoint Parallelism: Instances over disjoint participant sets may run concurrently.
- Per-Period Order: Each instance has a monotonically increasing sequence number within the period, assigned when scheduled.
- Chain Monotonicity: For any chain `r`, the subsequence of tags that include `r` is strictly increasing.
