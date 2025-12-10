# Should Rollups Work Without the Shared Publisher?

**TL;DR: Yes.** Rollups should keep producing local blocks when SP is down. Cross-chain stuff stops, but users can still transact locally.

---

## The Problem

Right now, op-geth blocks on startup waiting for SP connection. This means:

- Can't test the op-geth without running SP
- Single point of failure for the entire network
- Bad UX if SP has any downtime

## What the Spec Actually Says

The spec doesn't forbid local operation. Looking at the key quotes:

> "Native Sequencers: one per rollup, who **builds L2 blocks at a self-chosen frequency**"

> "Rollups may **keep independent L2 block times**; only period boundaries are common."

SP is needed for:

- Cross-chain transactions (SCP coordination)
- Period/superblock synchronization
- Settlement proof aggregation

SP is **NOT** needed for:

- Local block production
- Local transaction processing
- Running the EVM

The fault model says, "SP must be live to guarantee termination" – but that's about cross-chain tx completion, not local operation.

---

## Proposed Solution: Solo Mode

### Operating Modes

```
┌────────────────────────────────────────────────────────────────┐
│                                                                │
│   CONNECTED MODE (SP Available)                                │
│   ─────────────────────────────                                │
│   • Local transactions: YES                                    │
│   • Cross-chain transactions: YES                              │
│   • Period/Superblock tags: From SP (StartPeriod)              │
│   • Settlement: Active                                         │
│                                                                │
├────────────────────────────────────────────────────────────────┤
│                                                                │
│   SOLO MODE (SP Unavailable)                                   │
│   ─────────────────────────                                    │
│   • Local transactions: YES                                    │
│   • Cross-chain transactions: REJECTED (return error to user)  │
│   • Period/Superblock tags: Derived from L1 + genesis config   │
│   • Settlement: Deferred until SP reconnects                   │
│                                                                │
└────────────────────────────────────────────────────────────────┘
```

### Mode Transitions

```
                    ┌─────────────────┐
         ┌────────►│  CONNECTED MODE │◄────────┐
         │         └────────┬────────┘         │
         │                  │                  │
         │     SP heartbeat │                  │
         │        timeout   │                  │ SP reconnects
         │                  │                  │ + StartPeriod
         │                  ▼                  │
         │         ┌─────────────────┐         │
         └─────────│    SOLO MODE    │─────────┘
                   └─────────────────┘
```

---

## How It Works

### Startup Sequence

```
                         Sequencer Start
                               │
                               ▼
                    ┌─────────────────────┐
                    │  Load last finalized │
                    │  state from L1       │
                    └──────────┬──────────┘
                               │
                               ▼
                    ┌─────────────────────┐
                    │  Attempt SP connect  │
                    │  (timeout: TBD)      │
                    └──────────┬──────────┘
                               │
              ┌────────────────┴────────────────┐
              │                                 │
              ▼                                 ▼
     ┌─────────────────┐               ┌─────────────────┐
     │  SP Connected   │               │  Timeout/Failed │
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

### Period Derivation (when SP is down)

```go
func derivePeriod(genesisTime, now time.Time) PeriodID {
    elapsed := now.Sub(genesisTime)
    return PeriodID(elapsed / 3840 * time.Second)
}
```

### Handling Cross-Chain Requests in Solo Mode

Reject them with a clear error:

```go
if mode == SoloMode {
    return errors.New("cross-chain transactions unavailable: SP not connected")
}
```

---

## Recovery: When SP Reconnects

When SP comes back online, the sequencer receives `StartPeriod` and needs to reconcile state.

### Recovery Scenarios

**Scenario 1: States Match (Happy Path)**
```
Solo Mode derived:    Period 42, Superblock 142
SP says:              Period 42, Superblock 142

→ Resume Connected Mode, nothing special needed
```

**Scenario 2: Sequencer Behind SP**

Other rollups kept finalizing while this one was in Solo Mode.

```
Solo Mode derived:    Period 42, Superblock 142
SP says:              Period 44, Superblock 144

→ Sync forward to SP's view
→ Solo blocks still valid, just tagged with old period
→ Will be included in next settlement
```

**Scenario 3: Sequencer Ahead of SP**

SP was down, came back, and triggered a rollback (proof timeout).

```
Timeline:
- Period 100: Last finalized superblock (on L1)
- Period 101: SP goes down
- Period 102: Sequencer in Solo Mode, produces blocks for "Superblock 102"
- Period 103: SP comes back, proof for 101 failed → rollback to Superblock 100
            → SP now says: Period 103, Superblock 101

Solo Mode state:      targetSuperblock = 102+
SP says:              targetSuperblock = 101

→ Solo Mode blocks (101-102) will NOT be finalized
→ They're orphaned (same as a reorg)
→ Transactions return to mempool, get re-included
→ Sequencer syncs to SP's view and continues
```

### Recovery Logic

```go
func (s *Sequencer) onStartPeriod(msg StartPeriod) error {
    if s.mode == SoloMode {
        // Log if we produced blocks that won't finalize
        if s.targetSuperblock > msg.SuperblockNumber {
            s.log.Warn("Solo mode blocks may be orphaned",
                "local_superblock", s.targetSuperblock,
                "sp_superblock", msg.SuperblockNumber)
        }

        // SP is authoritative - sync to its view
        s.periodID = msg.PeriodID
        s.targetSuperblock = msg.SuperblockNumber
        s.mode = ConnectedMode
    }
    return nil
}
```

**Key insight:** No complex reconciliation needed. SP is authoritative, sequencer just follows. Solo Mode blocks were never finalized on L1, so orphaning them is safe.

---

## What Happens to Solo Mode Blocks?

They might get orphaned when SP comes back. That's OK:

- They were never "finalized" on L1
- Transactions return to mempool
- Get re-included in new blocks
- No funds lost

This is basically the same as a chain reorg.

---

## Limits

- **Max Solo Mode duration**: ~3 periods (~3.2 hours) then halt (TBD)
- **No cross-chain**: XTRequests rejected
- **No settlement**: Proofs deferred until SP returns
- **Blocks stay unsafe**: No finalization without SP

---

## Questions?

**Q: Is Solo Mode safe?**
Yes. Only local txs, no cross-chain state changes. L1 contract still enforces all security.

**Q: What if SP never comes back?**
Sequencers halt after max duration. User funds safe on L1. Deploy new SP and reconfigure.
