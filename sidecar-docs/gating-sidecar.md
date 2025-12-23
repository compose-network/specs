# Gating Sidecar

A sidecar that sits between `op-node` and `op-geth`, intercepting Engine API calls to satisfy Compose sequencer demands without forking the execution client.

## The Insight

op-geth can't build a block until op-node sends `forkchoiceUpdated` (FCU). By intercepting and holding FCU, we get exclusive control — no transactions can be included while we decide what to do with an XT.

## Architecture

```
op-node ──Engine API──▶ Gating Sidecar ──Engine API──▶ op-geth
                              │
                              └── Compose (SP, SCP)
```

## Flow

The sidecar maintains a pool of pending XTs.

When FCU arrives:
1. While pool is not empty, for up to 500ms:
   - Pop highest-paying XT
   - Simulate via `debug_traceCall` on op-geth (with state overrides from previous XTs)
   - Trace mailbox reads/writes, exchange CIRC messages, vote
   - Wait for `Decided`
   - If yes: add to committed bundle, accumulate state changes
   - If no: discard
2. Forward FCU with `transactions=[committed bundles]`
3. `op-geth` builds block with XTs at top, then fills with local transactions

## Sequential Simulation

When multiple XTs are processed in the same block, each must simulate on the state **after** previous XTs. Otherwise, if XT-A and XT-B both touch the same storage, B's simulation won't match its actual execution.

**Solution:** Chain `debug_traceCall` with state overrides.

```
XT-A: debug_traceCall(A, "latest", {tracer: "prestateTracer", diffMode: true})
      → Get state diff (what A changed)

XT-B: debug_traceCall(B, "latest", {
        tracer: "prestateTracer",
        diffMode: true,
        stateOverrides: {changes from A}
      })
      → B simulates on state after A
```

The `prestateTracer` with `diffMode: true` returns both `pre` and `post` state. We diff them to build `stateOverrides` for the next XT:

```json
{
  "0xMailbox": {
    "stateDiff": {
      "0x1": "0x1234..."
    }
  }
}
```

For mailbox tracing, we also call with `callTracer` (same state overrides) to see internal calls.

**Edge cases to handle:**
- New contracts created by previous XT (include `code` in override)
- Nonce changes if same sender has multiple XTs
- Balance changes from value transfers

**Simpler alternative:** Process one XT per block. No sequential simulation needed, but lower throughput.

## Satisfying the Demands

| Demand | How |
|--------|-----|
| Lock state | Hold FCU → op-geth can't build |
| Simulate | `debug_traceCall` on op-geth (standard RPC) |
| Guaranteed inclusion | `PayloadAttributes.transactions` forces bundle first |
| Set chain head | Requires small op-node hook (see below) |

## The `op-node` Hook

For SBCP rollbacks, the sidecar needs to tell op-node to revert to a previous block. op-node derives its head from L1, so it won't do this on its own.

A minimal hook:
```
POST /compose/setHead { "blockHash": "0x..." }
```

This is the only change needed to op-node.

## Tradeoffs

**What we get:**
- No op-geth fork
- Simple, auditable design
- Stock Engine API semantics

**What we give up:**
- XTs land at block boundaries (not sub-second)
- Blocks pause during XT processing
