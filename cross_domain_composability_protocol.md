# Cross-Domain Composability Protocol (CDCP)

This protocol enables atomic composability between rollups of the Compose network and rollups outside of it.
The atomicity property refers to the guarantee that either both rollups successfully include transactions from a user's intent, or neither does.

## Table of Contents

- [Native vs. External Rollups](#native-vs-external-rollups)
- [System Model](#system-model)
- [Mailbox](#mailbox)
- [Protocol](#protocol)
  - [Messages](#messages)
  - [Pseudo-code](#pseudo-code)
  - [Transitive Dependency & Sessions](#transitive-dependency--sessions)
- [WS - ER Sync](#ws---er-sync)
- [Settlement](#settlement)
  - [WS ZK Program](#ws-zk-program)
  - [SP Program Modifications](#sp-program-modifications)

## Native vs. External Rollups

To differentiate between types of rollups, we use the following terminology:
- **Native Rollups**: Rollups that are part of the Compose network.
- **External Rollups**: Rollups that are not part of the Compose network.

## System Model

We system encompasses 5 main components:
1. **User**: An entity that requests a cross-domain transaction execution.
2. **Shared Publisher (SP)**: The coordinator of the network that leads the execution of the protocol.
3. **Native Rollup Sequencers (NSs)**: Sequencers for the participating native rollups.
4. **External Rollup Client (ER)**: A client from the external rollup that accepts transactions to be included in a block.
5. **Wrapped Sequencer (WS)**: An entity that represents the participating external rollup sequencer in the Compose network, having access to the state and execution results of the external rollup.

The SP, NSs, and WS are part of the Compose network and are expected to have direct communication channels between them.
It's assumed that the user sends their request to SP, who will have the responsibility to initiate the protocol execution.
The WS is expected to have a communication channel with the ER.

For now, no byzantine faults are considered in the system.

## External Mailbox

Native rollups are assumed to have the standard [`Mailbox`](TODO) contract deployed.
External rollups will have a slightly modified version of the `Mailbox` contract.

In the `ExternalMailbox`, all messages are pre-populated by the WS:
- `read` is similar to the standard `Mailbox`, in which it's confirmed that the message exists and its data is returned.
- `write` is modified to ensure that the written message was pre-populated by the WS.

We will see how this pre-population works in the protocol description.
Next, we present the `ExternalMailbox` contract.

```text
CONTRACT ExternalMailbox:

    STRUCT MessageHeader:
        uint chainSrc
        uint chainDest
        address sender
        address receiver
        uint sessionId
        bytes label

    CONSTANT COORDINATOR (immutable address)

    STORAGE:
        array<uint> chainIDsInbox
        array<uint> chainIDsOutbox
        map<uint => bytes32> inboxRootPerChain
        map<uint => bytes32> outboxRootPerChain
        map<bytes32 => bytes> inbox
        map<bytes32 => bytes> outbox
        map<bytes32 => bool> createdKeys
        map<bytes32 => bool> usedKeys


    FUNCTION getKey(srcChainID, destChainID, sender, receiver, sessionId, label)
        RETURN keccak256(encode(srcChainID, destChainID, sender, receiver, sessionId, label))


    FUNCTION putInbox(srcChainID, sender, receiver, sessionId, label, data)
        REQUIRE caller == COORDINATOR

        key = getKey(srcChainID, currentChainID, sender, receiver, sessionId, label)

        IF createdKeys[key] == true:
            REVERT "Key already exists"

        inbox[key] = data
        createdKeys[key] = true


    FUNCTION putOutbox(destChainID, sender, receiver, sessionId, label, data)
        REQUIRE caller == COORDINATOR

        key = getKey(currentChainID, destChainID, sender, receiver, sessionId, label)

        IF createdKeys[key] == true:
            REVERT "Key already exists"

        outbox[key] = data
        createdKeys[key] = true


    FUNCTION read(srcChainID, sender, sessionId, label)
        key = getKey(srcChainID, currentChainID, sender, caller, sessionId, label)

        IF createdKeys[key] == false:
            REVERT MessageNotFound

        IF usedKeys[key] == true:
            REVERT MessageAlreadyUsed

        usedKeys[key] = true
        data = inbox[key]
        
        # Message is valid to be read.

        IF srcChainID not in inboxRootPerChain:
            append srcChainID to chainIDsInbox

        inboxRootPerChain[srcChainID] =
            keccak256(encode(inboxRootPerChain[srcChainID], key, data))

        RETURN data


    FUNCTION write(destChainID, receiver, sessionId, label, data)
        key = getKey(currentChainID, destChainID, caller, receiver, sessionId, label)

        IF createdKeys[key] == false:
            REVERT MessageNotFound

        IF usedKeys[key] == true:
            REVERT MessageAlreadyUsed

        IF hash(outbox[key]) != hash(data):
            REVERT MessageDataMismatch
            
        # Message is was pre-populated and so the write operation is valid to be executed.

        usedKeys[key] = true
        data = outbox[key]

        IF destChainID not in outboxRootPerChain[destChainID]:
            append destChainID to chainIDsOutbox

        outboxRootPerChain[destChainID] =
            keccak256(encode(outboxRootPerChain[destChainID], key, data))
```

> [!NOTE]
> In the Solidity contract, the storage layout of the `chainIDsInbox`,
> `chainIDsOutbox`, `inboxRootPerChain`, and `outboxRootPerChain` variables are
> used in the settlement protocol, which perform `eth_getProof` calls to retrieve
> the contract storage state. Thus, it's recommended that these varibles
> occupy the 1st, 2nd, 3rd and 4th slots, respectively.

> [!TIP]
> Space optimizations can be made to the contract by letting the `read` and `write` calls automatically remove used messages from storage.

## Protocol

Similarly to the SCP protocol, the SP initiates the execution by sending a start message, `StartCDCP`, to NSs and WS.

Then, each NS runs **exactly** the protocol rules of the [SCP protocol](TODO). Namely:
1. Once it receives the `StartCDCP` message from the SP, it starts a timer and selects the transactions from the `xD_transactions` list that are meant for its chain.
2. Then, it simulates its transactions, meaning that it executes them with a tracer at the mailbox, so that it can intercept `mailbox.Read` and `mailbox.Write` operations.
3. Once a `mailbox.Write` operation is intercepted, it sends a `Mailbox` message to the couterparty chain sequencer (either the WS or another NS).
4. Whenever a `mailbox.Read` operation is triggered and fails, it waits until a mailbox message is received.
5. Once a mailbox message is received from another sequencer, it adds a `mailbox.putInbox' transaction with it, placing it before the main transaction in the transaction list. Then, it goes back to step 2, re-starting the transaction simulation.
6. In case the transaction simulation is successful, it stops the timer (as it no longer will be used) and sends a `Vote(1)` message to the SP, indicating its willingness to include the transaction.
7. In case there's a timeout (before a `Vote` message has been sent) or if the transaction simulation fails but not due to a mailbox read error, it sends a `Vote(0)` message to the SP, indicating the impossibility of including the transaction, and terminates.
8. In case a `Decided(1)` message is received from the SP, it adds all `mailbox.putInbox' transactions created and the main transaction to the block, and terminates.
9. In case a `Decided(0)` message is received from the SP, it removes all `mailbox.putInbox' transactions created and the main transaction from the block, reverting to the previous state, and terminates.

The Shared Publisher (SP) runs a slightly modified version of the SCP protocol:
1. It sends a `StartCDCP` message to the appropriate sequencers, starts a timer and waits for NSs' `Vote` messages.
2. If a timeout occurs or if a `Vote(0)` message is received (indicates a failure), it sends a `Decided(0)` message to the NSs and a `NativeDecided(0)` message to the WS, and terminates.
3. If it receives a `Vote(1)` message from all NSs, it stops the timer (as it not longer will be used) and sends a `NativeDecided(1)` message to the WS, indicating that the NSs are willing to proceed.
4. Once a `WSDecided(b)` message is received from the WS, it sends a `Decided(b)` message indicating the result to the NSs, and terminates.

Note that, from the above, the SP works as a middle layer between the NSs and WS.
It indicates the viability results from the NSs to the WS, who is now allowed to communicate with the external rollup for including its transaction.
Once the WS receives a response, it indicates the result to the SP, who transmits the result back to the NSs.

The Wrapped Sequencer (WS) has the following rules:
1. Same as NS.
2. Same as NS.
3. Whenever a `stagedMailbox.Write` operation is intercepted and fails:
  - It sends a `Mailbox` message to the counterparty chain sequencer.
  - It creates a `stagedMailbox.putOutbox` transaction and adds it locally to the transaction list, placing it before the main transaction.
  - Then, it re-starts the transaction simulation going back to step 2.
4. Same as NS.
5. Same as NS.
6. In case the transaction simulation is successful, it waits for a `NativeDecided` message from the SP.
7. In case there's a timeout (before a `WSDecided` message has been sent) or if the transaction simulation fails but not due to a mailbox write or read error, it sends a `WSDecided(0)` message to the SP, indicating failure and terminates.
8. If a `NativeDecided(0)` message is received from the SP, it removes its transaction (and any created `stagedMailbox`) and terminates.
9. If a `NativeDecided(1)` message is received from the SP and its local transaction simulation is successful, it stops the timer (as it no longer will be used) and sends a special transaction to the ER, which populates the mailbox and executes the transaction atomically.
10. If the ER transaction fails, it sends a `WSDecided(0)` message to the SP, indicating failure and terminates.
11. If the ER transaction is successful, it sends a `WSDecided(1)` message to the SP, indicating success and terminates.

The special atomic transaction to the ER, `safe_execute`, allows an atomic execution of the mailbox staging and the main transaction.
It has the following pseudo-code:
```solidity
function safe_execute(stagedInboxMsgs, stagedOutboxMsgs, mainTx) external {
    // 1. Pre-populate the staged inbox messages
    for (msg in stagedInboxMsgs) {
        stagedMailbox.putInbox(msg...)
    }
    // 2. Pre-populate the staged outbox messages
    for (msg in stagedOutboxMsgs) {
        stagedMailbox.putOutbox(msg...)
    }
    // 3. Execute the main transaction
    call mainTx();
}
```

![cdcp](./images/cdcp.png)

### Messages

Besides the messages already defined in the [SCP protocol](TODO), we have the following additional messages:

```protobuf
message StartCDCP {
    uint64 SuperblockNumber = 1;
    uint64 xTSequenceNumber = 2;
    xTRequest xTRequest = 3;
    bytes xTid = 4; // 32-byte SHA256 hash over xTRequest
}
message NativeDecided {
    uint64 SuperblockNumber = 1;
    bytes xTid = 2;
    bool Decision = 3;
}
message WSDecided {
    uint64 SuperblockNumber = 1;
    bytes xTid = 2;
    bool Decision = 3;
}
```

### Pseudo-code

**CDCP Algorithm for the Shared Publisher**

```py
enum SPState { INIT, WAIT_NATIVE, WAIT_WS, DONE }

# ===== Per-xTid record =====
record SPContext {
  state: SPState
  superblock: uint64
  xTid: bytes32
  xReq: XTRequest
  nativeChains: Set<ChainID>    // the NSs expected to vote
  votes: Map<ChainID, bool>     // chainID -> decision
  timer: TimerHandle
  wsAddress: Address            // WS responsible for external rollup
  timeoutMs: uint64
}

# ===== API =====
function SP_StartCDCP(superblock, seqNo, xReq, xTid, nativeChains, wsAddress, timeoutMs):
  ctx = new SPContext
  ctx.state = INIT
  ctx.superblock = superblock
  ctx.xTid = xTid
  ctx.xReq = xReq
  ctx.nativeChains = nativeChains
  ctx.wsAddress = wsAddress
  ctx.timeoutMs = timeoutMs

  # 1. Broadcast StartCDCP to all NSs and the WS
  broadcast_to(nativeChains, StartCDCP{superblock, seqNo, xReq, xTid})
  send(wsAddress, StartCDCP{superblock, seqNo, xReq, xTid})

  # 2. Move to WAIT_NATIVE and set timer
  ctx.state = WAIT_NATIVE
  ctx.timer = set_timer(xTid, timeoutMs)


# ===== Event Handlers =====

# NS -> SP Votes
on_receive(fromNS, Vote{superblock, xTid, chainID, decision}):
  if ctx.state != WAIT_NATIVE: return

  # Ignore unexpected chain IDs
  if chainID not in ctx.nativeChains: return

  # Ignore duplicate votes
  if chainID in ctx.votes: return

  # Record vote
  ctx.votes[chainID] = decision

  # Fast-fail: any 0 vote or duplicate negative overrides success
  if decision == 0:
    cancel_timer(ctx.timer)
    # Failure path: tell NSs and WS
    broadcast_to(ctx.nativeChains, Decided{ctx.superblock, xTid, 0})
    send(ctx.wsAddress, NativeDecided{ctx.superblock, xTid, 0})
    ctx.state = DONE
    return

  # Check if all NSs voted 1
  if all(chain in ctx.votes and ctx.votes[chain] == true for chain in ctx.nativeChains):
    # All-positive: inform WS that NSs are ready
    cancel_timer(ctx.timer)
    send(ctx.wsAddress, NativeDecided{ctx.superblock, xTid, true})
    # Wait for WS decision to finalize
    ctx.state = WAIT_WS


# WS -> SP Final decision
on_receive(fromWS, WSDecided{superblock, xTid, Decision}):
  if ctx.state != WAIT_WS: return

  # Relay result to NSs
  broadcast_to(ctx.nativeChains, Decided{ctx.superblock, xTid, Decision})
  ctx.state = DONE


# Timeouts
on_timer_fired(xTid):
  if ctx.state == WAIT_NATIVE:
    # Timeout before native unanimity
    broadcast_to(ctx.nativeChains, Decided{ctx.superblock, xTid, 0})
    send(ctx.wsAddress, NativeDecided{ctx.superblock, xTid, 0})
    ctx.state = DONE
```

**CDCP Algorithm for the Wrapped Sequencer**
```py

enum WSState { INIT, SIMULATING, WAIT_NATIVE_DECIDED, SUBMIT_TO_ER, DONE }

# Local simulation hooks interface:
function simulate_with_mailbox_tracer(txList, handlers) -> SimResult
# SimResult: { success: bool, failReason: enum{NONE, READ_MISS, WRITE_MISS, OTHER}, readContext?, writeContext? }

# Driver that creates the final atomic ER transaction that:
#  - pre-populates staged inbox/outbox
#  - executes the main transaction
function build_and_send_ER_atomic_tx(ctx) -> ERResult

# ===== Per-session record =====
record WSContext {
  chainID: uint32
  state: WSState
  superblock: uint64
  xTid: bytes32
  xReq: XTRequest

  txMain: Transaction                   # transaction for the external rollup
  mailboxTxs: List<Transaction>         # Mailbox txs that are staged before txMain
  pendingMailboxMsgs: List<MailboxMsg>  # to send to counterpart chains
  timer: TimerHandle
  timeoutMs: uint64
  nativeReady: bool                     # received NativeDecided(1)
}

# ===== Session start =====
on_receive(StartCDCP{superblock, xTSequenceNumber, xTRequest, xTid} from SP):
  ctx = new WSContext
  ctx.state = INIT
  ctx.superblock = superblock
  ctx.xTid = xTid
  ctx.xReq = xTRequest
  ctx.txMain = extract_transactions(xTRequest, ctx.chainID)  # select the ER transaction
  ctx.mailboxTxs = []
  ctx.pendingMailboxMsgs = []
  ctx.nativeReady = false

  # enter simulation
  ctx.state = SIMULATING
  ctx.timer = set_timer(xTid, ctx.timeoutMs)
  WS_run_simulation(xTid)


# ===== Simulation routine =====
function WS_run_simulation(xTid):
  if ctx.state != SIMULATING: return

  sim = simulate_with_mailbox_tracer(ctx.mailboxTxs + [ctx.txMain], handlers)

  if sim.success:
    # Wait for NativeDecided
    ctx.state = WAIT_NATIVE_DECIDED
    if ctx.nativeReady:
      ctx.state = SUBMIT_TO_ER
      WS_submit_to_ER(xTid)
    return

  if sim.failReason == WRITE_MISS:

    m = make_mailbox_msg_from_write(sim.writeCtx, ctx.xTid)
    send_mailbox_to_counterparty(m)

    s = StagedOutboxWrite{
        destChainID: m.destChainID,
        sender: m.sender,
        receiver: m.receiver,
        sessionId: m.sessionId,
        label: m.label,
        data: m.data
    }
    ctx.mailboxTxs.append(s)

    # Restart simulation after updating the list
    return WS_run_simulation(xTid)

  # Failure cases:
  if sim.failReason == READ_MISS:
    # Wait for mailbox message arrival from another sequencer
    return

  else if sim.failReason == OTHER:
    # Irrecoverable local failure
    cancel_timer(ctx.timer)
    send(SP, WSDecided{ctx.superblock, ctx.xTid, false})
    ctx.state = DONE
    return


# ===== Mailbox message arrival from counterpart chain (enables read) =====
on_receive(Mailbox msg from counterpart, xTid):

  if ctx.state == SIMULATING:
    s = StagedInboxRead{
      srcChainID: msg.srcChainID,
      sender: msg.sender,
      receiver: msg.receiver,
      sessionId: msg.sessionId,
      label: msg.label,
      data: msg.data
    }
    ctx.mailboxTxs.append(s)
    WS_run_simulation(xTid)


# ===== NativeDecided from SP =====
on_receive(NativeDecided{superblock, xTid, Decision} from SP):
  if superblock != ctx.superblock: return

  if Decision == 0:
    # Abort locally and don't include the txs
    cancel_timer(ctx.timer)
    ctx.mailboxTxs.clear()
    ctx.state = DONE
    return

  # Decision == 1: NSs are willing; proceed only if our local sim was already successful
  if ctx.state == WAIT_NATIVE_DECIDED:
    ctx.nativeReady = true
    ctx.state = SUBMIT_TO_ER
    WS_submit_to_ER(xTid)
  else:
    # Mark readiness and let simulation finish
    ctx.nativeReady = true


# ===== After local success + NativeDecided(1): submit atomic tx to ER =====
function WS_submit_to_ER(xTid):
  if ctx.state != SUBMIT_TO_ER: return

  cancel_timer(ctx.timer)

  # Call ER with atomic tx
  erRes = build_and_send_ER_atomic_tx(ctx)

  if erRes.success:
    # Rule 11: success
    send(SP, WSDecided{ctx.superblock, ctx.xTid, 1})
  else:
    # Rule 10: failure
    send(SP, WSDecided{ctx.superblock, ctx.xTid, 0})

  ctx.state = DONE


# ===== Timeout =====
on_timer_fired(xTid):
  if ctx.state in {SIMULATING, WAIT_NATIVE_DECIDED}:
    send(SP, WSDecided{ctx.superblock, ctx.xTid, 0})
    ctx.state = DONE
```

### Transitive Dependency & Sessions

In its initial version, the protocol only supports one external rollup.

In future versions, many will be supported.
Due to the settlement dependency, a proper session management system will be required not to create settlement deadlocks, that could allow double-spending and other attacks.


## WS - ER Sync

For simulating transactions, the WS needs a snapshot of the ER's state, which influences the contents of any message written to other chains.
Once the `safe_execute` tx is submitted to the ER, it will be re-executed with the ER's current state, and it will only succeed if the written messages are the same as the ones simulated (pre-populated in the ExternalMailbox).
Therefore, to maximize the probability of success, the WS should have the most recent state possible.

For that, the WS will be constantly syncing the ER's state.
However, note that the state can't be updated during the protocol's execution, as it could change the simulation results (previously written messages).
Thus, the protocol initiation should also lock the chain's state being used.

**Design pattern**<br>
- The WS keeps **one active global snapshot** (read-only) + zero or one **staging snapshot**.
- CDCP instances acquire a read lease on the active snapshot at start, and release it at end.
- A background sync loop fetches the newest ER state continuously, but updates `active <- staging` only when there's no active readers (leases).

This represents a reader/writer with versioned snapshots setup, which doesn't require the complexity of a RWLock as sessions never need to write, i.e. only the writer swaps when there are no active readers.

```py
# ===== ER State Snapshot =====
record ERStateSnapshot {
  uint64  height
  bytes32 block_hash
  State   state           # abstract state representation
}

# ===== Snapshot Store with Leasing =====
record SnapshotStore {
  ERStateSnapshot active              # globally visible snapshot
  ERStateSnapshot? staging            # newest fetched, not yet active
  bool   read_lease                   # true if any CDCP session is using active
  bool   swap_pending                 # true if staging exists and waiting to swap
  Mutex  mu
  time   last_swap_time
}

function WS_BackgroundSyncLoop(store: SnapshotStore, erClient: ERClient, poll_interval_ms):
  while true:
    # Fetch latest ER view
    latest = fetch_latest_er_snapshot(erClient)

    lock(store.mu):
      # If this is not fresher or same block, skip
      if store.staging != null and
            (latest.height < store.staging.height or
            (latest.height == store.staging.height and latest.block_hash == store.staging.block_hash)
            ):
        unlock(store.mu)
        sleep(poll_interval_ms)
        continue

      # Put into staging
      store.staging = latest
      store.swap_pending = true

      # If there are no readers, swap immediately
      if store.read_lease == false:
        store.active = store.staging
        store.staging = null
        store.swap_pending = false
        store.last_swap_time = now()

      unlock(store.mu)

    sleep(poll_interval_ms)

# Acquire a read-only snapshot lease for a new CDCP session.
# Returns a frozen pointer/copy to ERStateSnapshot the session must use throughout.
function acquire_snapshot_lease(store: SnapshotStore) -> ERStateSnapshot?:
  lock(store.mu):
    store.read_lease = true
    snapshot = store.active
  unlock(store.mu)
  return snapshot

# Release the lease when the session finishes.
function release_snapshot_lease(store: SnapshotStore):
  lock(store.mu):
    store.read_lease = false
    if store.read_lease == false and store.swap_pending:
      # Safe swap now
      store.active = store.staging
      store.staging = null
      store.swap_pending = false
      store.last_swap_time = now()
  unlock(store.mu)
```

## Settlement

Following the SBCP (v2), at the end of the superblock period, sequencers submit an `AggregationProof` to the SP,
commiting to their final state and to the associated mailbox roots.
With an external rollup, the mailbox roots from natives should also be compared to the roots stores in the ER.

Therefore, the WS should provide the SP with the ER's staged mailbox roots at settlement time.
While the WS's proof does not need to attest to the ER's correct state execution, it should
at least prove that there's a certain mailbox state associated with a certain ER block hash and number.

### WS ZK Program

For that, the WS zk program input is structured as:
```rust
pub struct WSInput {
  WitnessData, // With similar data as in the ZK Range program input
  pub post_state_root: B256,
  
  // Inbox
  pub mailbox_inbox_chains_len: GetProofOutput
  pub mailbox_inbox_chains:     GetProofOutput, // List of chains in inbox
  pub mailbox_inbox_roots:      GetProofOutput, // List of inbox root per chain
  
  // Outbox
  pub mailbox_outbox_chains_len:    GetProofOutput
  pub mailbox_outbox_chains:        GetProofOutput, // List of chains in outbox
  pub mailbox_outbox_roots:         GetProofOutput, // List of outbox root per chain
}

pub struct GetProofOutput {
  pub address: B256,
  pub accountProof: Vec<B256>, // Merkle proof
  pub storageHash: B256,
  pub storageProof: Vec<StorageProof>
}

pub struct StorageProof {
  pub key: B256,
  pub value: B256,
  pub proof: Vec<B256>, // Merkle proof
}
```

It consists of a `post_state_root` block reference and several [`eth_getProof`](https://www.quicknode.com/docs/ethereum/eth_getProof)
outputs for fetching the mailbox state described in the `ExternalMailbox` contract.

More precisely:
- The `post_state_root` field represents the last ER's block state root associated to the superblock period.
- The `mailbox_inbox_chains_len` represents the output of calling `eth_getProof` for the slot `0x0`, which stores the length of the `chainIDsInbox` array (`len_inbox`).
- Similarly, `mailbox_outbox_chains_len` represents the call at slot `0x1` for the length of the `chainIDsOutbox` array (`len_outbox`).
- Given these lengths, the WS can fill `mailbox_inbox_chains` which represents a call for `len_inbox` items with keys `key(i) = base + i`, where `base = keccak256(0x0)`.
- The same goes for `mailbox_outbox_chains`, with `base = keccak256(0x1)`.
- The `StorageProof.value`, for the above calls items, represent the chain IDs that have messages in the inbox and outbox, respectively.
- Finally, `mailbox_inbox_roots` and `mailbox_outbox_roots` represent calls for getting the data in `inboxRootPerChain` and `outboxRootPerChain`, respectively.
- Each is called for `len_inbox` and `len_outbox` items, respectively, with keys `key(chainID) = keccak256(chainID || base)` for `base` as `0x02` and `0x03`, respectively.

The program will produce the following output, committing to the mailbox state root, as defined in the [settlement layer spec](./settlement_layer.md).
```rust
pub struct WSOutput {
  pub post_state_root:  B256,
  pub mailbox_contract: B256,
  pub mailbox_root:     B256,
}
```

Once a proof is generated, the WS submits it to the SP along with the list of chain-specific inbox and outbox roots, as other native sequencers do.

**WS ZK Program Pseudocode**
```py
procedure WS_ZK_Program(input: WSInput) -> WSOutput:
    // Confirm post_state_root exists in the ER contract
    ensure_l2_block_exists(input.witness_data, input.post_state_root)
    
    // Verify that mailbox is consistent and get it
    mailbox_addr = verify_consistency_and_return_mailbox_address(input)
    
    // Verify proofs
    verify_get_proof(input.mailbox_inbox_chains_len, input.post_state_root)
    verify_get_proof(input.mailbox_inbox_chains, input.post_state_root)
    verify_get_proof(input.mailbox_inbox_roots, input.post_state_root)
    verify_get_proof(input.mailbox_outbox_chains_len, input.post_state_root)
    verify_get_proof(input.mailbox_outbox_chains, input.post_state_root)
    verify_get_proof(input.mailbox_outbox_roots, input.post_state_root)
    
    // Reconstruct mailbox root
    inbox_len = input.mailbox_inbox_chains_len.storageProof[0].value
    outbox_len = input.mailbox_outbox_chains_len.storageProof[0].value
    inbox_chains = [sp.value for sp in input.mailbox_inbox_chains.storageProof]
    outbox_chains = [sp.value for sp in input.mailbox_outbox_chains.storageProof]
    ensure inbox_len = len(inbox_chains)
    ensure outbox_len = len(outbox_chains)
    
    inbox_roots = [sp.value for sp in input.mailbox_inbox_roots.storageProof]
    outbox_roots = [sp.value for sp in input.mailbox_outbox_roots.storageProof]
    ensure inbox_len = len(inbox_roots)
    ensure outbox_len = len(outbox_roots)
    
    mailbox_root = compute_mailbox_root(inbox_chains, inbox_roots, outbox_chains, outbox_roots)
    
    commit(WSOutput{
    post_state_root: input.post_state_root,
    mailbox_contract: mailbox_addr,
    mailbox_root: mailbox_root
    })
```


### SP Program Modifications

The SP zk program should be adjusted to accept the WSOutput as an additional input.
The SP then needs to:
- Verify the proof for `WSOutput`.
- Verify that `WSOutput.mailbox_contract` matches the expected ExternalMailbox address for the WS.
- As done for other rollups, verify that the `WSOutput.mailbox_root` is correct considering list of mailbox roots, and match these roots against native ones. 
