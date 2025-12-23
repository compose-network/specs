# Compose Sidecar

## Purpose

Replace the current op-geth fork with a standalone component that doesn't require modifying the execution client.

## Demands from Sequencers

1. **Lock state**
   - Prevent local transactions while an instance is active.

2. **Simulate**
   - Run the XT with mailbox tracing, so sidecar can vote.

3. **Guaranteed inclusion**
   - If `yes`, include exactly where simulated.
   - If `no`, exclude entirely.

4. **Set chain head**
   - For rollback when settlement fails.
