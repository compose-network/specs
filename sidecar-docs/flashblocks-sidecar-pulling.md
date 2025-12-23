# Flashblocks Sidecar (Pull Model)

A sidecar that op-rbuilder **pulls** transactions from at flashblock boundaries, giving us guaranteed inclusion with sub-second latency.

## The Insight

Instead of pushing bundles and hoping they arrive in time, we flip control: the builder asks the sidecar for transactions at the start of each flashblock. While the builder waits for our response, its state is frozen — we simulate on exactly the state that will be used for execution.

```
Push (problematic):     Sidecar ──bundle──▶ Builder pool (might miss window)
Pull (this design):     Builder ──"any txs?"──▶ Sidecar ──"here"──▶ Builder executes
```

## Architecture

```
                    Compose Sidecar
                          │
           ┌──────────────┼──────────────┐
           │              │              │
           ▼              ▼              ▼
      op-rbuilder    op-rbuilder     Publisher
      (Chain A)      (Chain B)
           │              │
           ▼              ▼
     rollup-boost   rollup-boost
     (strict mode)  (strict mode)
```

## Flow

The sidecar coordinates XTs across chains using a hold/poll protocol:

1. **XT arrives** at sidecar from Publisher
2. **Chain A builder** starts flashblock, calls `POST /transactions`
3. **Sidecar responds** `{ hold: true }` — waiting for Chain B
4. **Chain B builder** starts flashblock, calls `POST /transactions`
5. **Sidecar has both frozen states** — simulates XT on both
6. **Sidecar votes** in 2PC, waits for decision
7. **On COMMIT**: both builders poll, get `{ transactions: [XT], required: true }`
8. **Builders execute** on frozen state (matches simulation), confirm back
9. **On ABORT**: both builders poll, get empty response, proceed normally

```
Timeline:
    T+0      T+50     T+150    T+200    T+300
    │        │        │        │        │
    XT       A calls  B calls  2PC      Both
    arrives  (holds)  (holds)  decides  execute
                      ↓
              Sidecar has both states,
              simulates, votes
```

## Satisfying the Demands

| Demand | How |
|--------|-----|
| Lock state | Builder blocked waiting for response → state frozen |
| Simulate | Sidecar simulates on frozen state (provided in request) |
| Guaranteed inclusion | `required: true` — builder must include or fail flashblock |
| Set chain head | Requires op-node hook (same as gating) |

## Required Forks

Unlike gating, this approach requires minimal forks to op-rbuilder and rollup-boost:

### op-rbuilder (~150 LOC)

Add "external transaction source" — at flashblock start, builder calls configured endpoint:

```
POST http://sidecar/transactions
{ chain_id, block_number, flashblock_index, state_root, ... }

Response: { hold: true, poll_after_ms: 50 }           // wait and retry
      or: { transactions: [...], required: true }     // execute these
      or: { transactions: [] }                        // proceed with mempool
```

Builder polls until `hold: false`, then executes any transactions before mempool.

### rollup-boost (~30 LOC)

Add "strict mode" — if builder fails, don't fall back to op-geth:

```toml
strict_mode = true  # Fail if builder fails, no L2 fallback
```

Without this, rollup-boost could silently use op-geth's block (which won't have the XT).

### op-node (~20 LOC)

Same as gating — hook for SBCP rollbacks:

```
POST /compose/setHead { "blockHash": "0x..." }
```

## The Guarantee

```
Builder blocked      → State frozen while we coordinate
Frozen state         → Simulation matches execution
required: true       → Builder includes TX or fails flashblock
Strict mode          → No silent fallback to op-geth
                     ────────────────────────────────
                     XT on both chains, or neither
```

## Compared to Gating

| | Gating | Flashblocks (Pull) |
|--|--------|-------------------|
| XT latency | Block time (2s) | ~300ms |
| During XT processing | Block paused | Only target flashblock paused |
| Forks required | None (sidecar only) | op-rbuilder, rollup-boost (minimal) |
| Simulation | op-geth `debug_traceCall` | On state from builder request |
| Complexity | Simpler | More moving parts |

## Tradeoffs

**What we get:**
- Sub-second XT latency (~300ms typical)
- Mempool keeps flowing (only specific flashblocks held)
- Strong inclusion guarantees (same as gating)
- Generic interface — potentially upstreamable

**What we give up:**
- Requires op-rbuilder fork (minimal, ~150 LOC)
- Requires rollup-boost strict mode (~30 LOC)
- More complex coordination than gating
- Flashblock timing variable during XTs

## Open Questions

1. **State in request** — Is `state_root` enough, or do we need more for simulation?
2. **Cross-chain timing** — What if chains are very out of sync (>400ms)?
3. **Timeout handling** — How long should builder wait before giving up?

## References

- [Detailed Design](./detailed/flashblocks-sidecar-pulling.md) — Full implementation spec
- [Validation Report](./flashblocks-compose-validation-report.md) — Analysis of op-rbuilder internals
- [Gating Sidecar](./gating-sidecar.md) — Simpler alternative
