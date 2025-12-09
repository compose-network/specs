# Should Rollups Work Without the Shared Publisher?

**TL;DR: Yes.** Rollups should keep producing local blocks when SP is down. Cross-chain stuff stops, but users can still
transact locally.

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

The fault model says, "SP must be live to guarantee termination" – but that's about cross-chain tx completion, not local
operation.

---

## Proposed Solution: Solo Mode

Two operating modes:

### Connected Mode (SP Available)

- Everything works normally
- Cross-chain txs via SCP
- Settlement happens on schedule

### Solo Mode (SP Unavailable)

- Local transactions work
- Cross-chain requests rejected with clear error
- Period/superblock derived from genesis time + L1 state
- Blocks marked as "unsafe" only (no finalization until SP returns) - im not sure about this

---

## How It Works

### Startup

```
1. Bootstrap from L1 (get last finalized superblock)
2. Try connecting to SP (X second timeout, TBD)
3. If connected → Connected Mode
4. If timeout → Solo Mode (derive period from genesis + current time)
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

### When SP Reconnects

1. Receive `StartPeriod` from SP
2. Compare local state vs SP state
3. If diverged: sync to SP's view (solo blocks may be orphaned – that's fine, they were never finalized)
4. Resume Connected Mode

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

- **Max Solo Mode duration**: ~3 periods (~3.2 hours) then halt TBD
- **No cross-chain**: XTRequests rejected
- **No settlement**: Proofs deferred until SP returns
- **Blocks stay unsafe**: No finalization without SP

---

## Questions?

**Q: Is Solo Mode safe?**
Yes. Only local txs, no cross-chain state changes. L1 contract still enforces all security.

**Q: What if SP never comes back?**
Sequencers halt after max duration. User funds safe on L1. Deploy new SP and reconfigure.
