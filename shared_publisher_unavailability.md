# Shared Publisher Unavailability <!-- omit from toc -->

This document specifies the expected behavior of Compose rollups when the Shared Publisher (SP) is unavailable. It defines a dual-mode operation model that allows rollups to maintain liveness for local transactions while gracefully degrading cross-chain functionality.

## Table of Contents <!-- omit from toc -->

- [Overview](#overview)
  - [Problem Statement](#problem-statement)
  - [Design Rationale](#design-rationale)
  - [Specification Basis](#specification-basis)
- [Operating Modes](#operating-modes)
  - [Mode Definitions](#mode-definitions)
  - [Mode Transitions](#mode-transitions)
- [Startup Behavior](#startup-behavior)
  - [Startup Sequence](#startup-sequence)
  - [L1 Bootstrap](#l1-bootstrap)
  - [Period Derivation (Solo Mode)](#period-derivation-solo-mode)
- [Runtime Behavior](#runtime-behavior)
  - [Local Transaction Processing](#local-transaction-processing)
  - [Cross-Chain Transaction Handling](#cross-chain-transaction-handling)
  - [Active SCP Instance Handling](#active-scp-instance-handling)
- [Recovery Protocol](#recovery-protocol)
  - [Recovery Trigger](#recovery-trigger)
  - [State Reconciliation](#state-reconciliation)
  - [Recovery Scenarios](#recovery-scenarios)
  - [Recovery Implementation](#recovery-implementation)
- [Block Safety](#block-safety)
  - [Safety Levels](#safety-levels)
  - [Safety Levels by Mode](#safety-levels-by-mode)
  - [Solo Mode Block Disposition](#solo-mode-block-disposition)
- [Operational Limits](#operational-limits)
- [Security Considerations](#security-considerations)
  - [Safety Properties](#safety-properties)
  - [Liveness Properties](#liveness-properties)
  - [Recovery Safety](#recovery-safety)
- [FAQ](#faq)

## Overview

### Problem Statement

The current implementation requires rollup nodes (op-geth) to establish SP connection before producing blocks. This creates:

- A hard dependency that blocks startup and testing without SP infrastructure
- A single point of failure affecting all network rollups
- Degraded availability during SP maintenance or outages

### Design Rationale

The Compose protocol architecture separates two concerns:

1. **Per-rollup sequencing**: Each rollup has its own sequencer for local block production
2. **Cross-rollup coordination**: SP coordinates atomic cross-chain transactions and settlement

This separation enables independent operation when coordination is unavailable.

### Specification Basis

The protocol specifications support independent local operation:

> [!NOTE]
> "Native Sequencers: one per rollup, who **builds L2 blocks at a self-chosen frequency**"
> — Superblock Construction Protocol

> [!NOTE]
> "Rollups may **keep independent L2 block times**; only period boundaries are common."
> — Superblock Construction Protocol

The fault model states "SP must be live to guarantee termination" — this constraint applies to cross-chain transaction completion, not local block production.

## Operating Modes

### Mode Definitions

```
┌────────────────────────────────────────────────────────────────┐
│                                                                │
│   CONNECTED MODE                                               │
│   ──────────────                                               │
│   SP Status: Available                                         │
│                                                                │
│   • Local transactions ................ ENABLED                │
│   • Cross-chain transactions .......... ENABLED                │
│   • Period/Superblock tags ............ From SP (StartPeriod)  │
│   • Settlement ........................ ACTIVE                 │
│                                                                │
├────────────────────────────────────────────────────────────────┤
│                                                                │
│   SOLO MODE                                                    │
│   ─────────                                                    │
│   SP Status: Unavailable                                       │
│                                                                │
│   • Local transactions ................ ENABLED                │
│   • Cross-chain transactions .......... DISABLED (error)       │
│   • Period/Superblock tags ............ Derived (L1 + genesis) │
│   • Settlement ........................ DEFERRED               │
│                                                                │
└────────────────────────────────────────────────────────────────┘
```

### Mode Transitions

```
                   ┌─────────────────┐
         ┌────────►│  CONNECTED MODE │◄────────┐
         │         └────────┬────────┘         │
         │                  │                  │
         │                  │ SP heartbeat     │
         │                  │ timeout          │ SP reconnects
         │                  │                  │ + StartPeriod
         │                  ▼                  │
         │         ┌─────────────────┐         │
         └─────────│    SOLO MODE    │─────────┘
                   └─────────────────┘
```

**Transition Conditions:**

| From      | To        | Trigger                              |
|-----------|-----------|--------------------------------------|
| Connected | Solo      | SP heartbeat timeout exceeded        |
| Solo      | Connected | Valid `StartPeriod` received from SP |

## Startup Behavior

### Startup Sequence

```
                         Sequencer Start
                               │
                               ▼
                    ┌──────────────────────┐
                    │  Bootstrap from L1   │
                    │  (last finalized     │
                    │   superblock state)  │
                    └──────────┬───────────┘
                               │
                               ▼
                    ┌──────────────────────┐
                    │  Attempt SP connect  │
                    │  (timeout: CONFIG)   │
                    └──────────┬───────────┘
                               │
              ┌────────────────┴────────────────┐
              │                                 │
              ▼                                 ▼
     ┌─────────────────┐               ┌─────────────────┐
     │  SP Connected   │               │  Timeout        │
     └────────┬────────┘               └────────┬────────┘
              │                                 │
              ▼                                 ▼
     ┌─────────────────┐               ┌─────────────────┐
     │  Receive        │               │  Derive period  │
     │  StartPeriod    │               │  from L1 state  │
     └────────┬────────┘               └────────┬────────┘
              │                                 │
              ▼                                 ▼
     ┌─────────────────┐               ┌─────────────────┐
     │  CONNECTED MODE │               │    SOLO MODE    │
     └─────────────────┘               └─────────────────┘
```

### L1 Bootstrap

On startup, the sequencer MUST query the L1 settlement contract for:

- `lastFinalizedSuperblockNumber`
- `lastFinalizedSuperblockHash`
- `lastFinalizedBlockHeader`

This establishes the base state regardless of SP availability.

### Period Derivation (Solo Mode)

When SP is unavailable, period and superblock values are derived deterministically:

```go
func derivePeriod(genesisTime, currentTime time.Time) PeriodID {
    elapsed := currentTime.Sub(genesisTime)
    return PeriodID(elapsed / PeriodDuration) // PeriodDuration = 3840s
}

func deriveTargetSuperblock(lastFinalizedSuperblock SuperblockNumber) SuperblockNumber {
    return lastFinalizedSuperblock + 1
}
```

## Runtime Behavior

### Local Transaction Processing

Local transactions MUST be processed in both modes. The sequencer:

1. Accepts transactions into mempool
2. Executes via EVM
3. Produces blocks at configured frequency
4. Emits block headers (Unsafe safety level in Solo Mode, see [Block Safety](#block-safety))

### Cross-Chain Transaction Handling

In Solo Mode, cross-chain transaction requests (XTRequest) MUST be rejected:

```go
if mode == SoloMode {
    return ErrCrossChainUnavailable
}
```

Error response MUST clearly indicate:

- Current mode status
- Reason for rejection
- Expected behavior when SP reconnects

### Active SCP Instance Handling

If SP disconnects during an active SCP instance:

1. Detect SP connection loss (heartbeat timeout)
2. Abort the active instance locally (treat as `Decided(false)`)
3. Clear tentative state and pending mailbox messages
4. Re-enable local transaction processing
5. Transition to Solo Mode

## Recovery Protocol

### Recovery Trigger

Recovery initiates when the sequencer receives a valid `StartPeriod` message from SP while in Solo Mode.

### State Reconciliation

The sequencer compares local derived state against SP-provided state:

```
Local State:  { periodID, targetSuperblock }
SP State:     { StartPeriod.PeriodID, StartPeriod.SuperblockNumber }
```

### Recovery Scenarios

**Scenario 1: State Match**

```
Local:   Period 42, Superblock 142
SP:      Period 42, Superblock 142

Action:  Resume Connected Mode
```

**Scenario 2: Sequencer Behind**

```
Local:   Period 42, Superblock 142
SP:      Period 44, Superblock 144

Action:  Sync forward to SP state
         Solo blocks remain valid (older period tag)
```

**Scenario 3: Sequencer Ahead**

```
Timeline:
  Period 100: Last finalized (L1)
  Period 101: SP offline
  Period 102: Solo Mode blocks produced
  Period 103: SP returns, rolled back to Superblock 100
            → SP announces: Period 103, Superblock 101

Local:   targetSuperblock = 102+
SP:      targetSuperblock = 101

Action:  Solo blocks (101-102) orphaned
         Transactions return to mempool
         Sync to SP state, continue
```

### Recovery Implementation

```go
func (s *Sequencer) onStartPeriod(msg StartPeriod) error {
    if s.mode == SoloMode {
        if s.targetSuperblock > msg.SuperblockNumber {
        s.log.Warn("solo mode blocks will be orphaned",
        "local_superblock", s.targetSuperblock,
        "sp_superblock", msg.SuperblockNumber)
        }

    // SP state is authoritative
    s.periodID = msg.PeriodID
    s.targetSuperblock = msg.SuperblockNumber
    s.mode = ConnectedMode
    }
    return nil
}
```

**Principle:** SP is authoritative. No complex reconciliation is required. Solo Mode blocks were never L1-finalized, so
orphaning is safe.

## Block Safety

### Safety Levels

As defined in the [Settlement Layer](./settlement_layer.md#superblock-and-l2-block-safety-levels) specification:

| Level         | Definition                                            |
|---------------|-------------------------------------------------------|
| **Unsafe**    | Superblock/block received through gossip protocol     |
| **Validated** | Proof for superblock received through gossip protocol |
| **Finalized** | Proof published to L1 settlement contract             |

L2 blocks inherit the safety level of their containing superblock.

### Safety Levels by Mode

| Mode      | Unsafe | Validated | Finalized |
|-----------|--------|-----------|-----------|
| Connected | Yes    | Yes       | Yes       |
| Solo      | Yes    | No        | No        |

In Solo Mode, blocks cannot progress beyond **Unsafe** because:

- No SP coordination means no superblock proof aggregation
- No proof means blocks cannot reach **Validated**
- Nothing published to L1 means no **Finalized** status

### Solo Mode Block Disposition

Blocks produced in Solo Mode:

- Are valid for local execution
- Carry derived period/superblock tags
- Remain at **Unsafe** safety level
- May be orphaned on SP reconnect (equivalent to chain reorganization)
- Transactions return to mempool if orphaned

## Operational Limits

| Parameter                | Value              | Notes                 |
|--------------------------|--------------------|-----------------------|
| Max Solo Mode Duration   | ~3 periods (~3.2h) | TBD; halt after limit |
| Cross-chain Transactions | Rejected           | Clear error returned  |
| Settlement               | Deferred           | Resumes on reconnect  |
| Block Finality           | Unsafe only        | No L1 finalization    |

## Security Considerations

### Safety Properties

Solo Mode maintains safety:

- Only local transactions are processed
- No cross-chain state modifications are possible
- L1 settlement contract enforces all finality guarantees
- Orphaned blocks do not affect L1-finalized state

### Liveness Properties

Solo Mode provides degraded liveness:

- Local transactions continue processing
- Cross-chain operations unavailable
- Settlement deferred but not lost

### Recovery Safety

On SP reconnect:

- SP state is authoritative
- Local divergence resolved by orphaning unfinalized blocks
- No double-spend possible (L1 is source of truth)

## FAQ

**Q: Is Solo Mode safe?**

Yes. Only local transactions are processed. Cross-chain state changes are impossible. The L1 settlement contract
enforces all security properties.

**Q: What happens if SP never recovers?**

Sequencers halt after maximum Solo Mode duration. User funds remain safe on L1. Resolution requires deploying a new SP
instance and reconfiguring sequencer connections.
