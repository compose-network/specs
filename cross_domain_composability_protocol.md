# Cross-Domain Composability Protocol (CDCP)

This protocol enables atomic composability between rollups of the Compose network and rollups outside of it.
The atomicity property refers to the guarantee that either both rollups successfully include transactions from a user's intent, or neither does.

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

## Mailbox

Native rollups are assumed to have the standard [`Mailbox`](TODO) contract deployed.
External rollups will have a slightly modified version of the `Mailbox` contract, called `StagedMailbox`.

In the `StagedMailbox`, all messages are pre-populated by the WS:
- `read` is similar to the standard `Mailbox`, in which it's confirmed that the message exists and its data is returned.
- `write` is modified to ensure that the written message was pre-populated by the WS.

We will see how this pre-population works in the protocol description.
Next, we present the `StagedMailbox` contract.

```solidity
contract StagedMailbox {

    struct MessageHeader {
        // Origin tuple
        uint256 chainSrc;
        uint256 chainDest;
        // Recipient tuple
        address sender;
        address receiver;
        // Nonce per user intent
        uint256 sessionId;
        // Label for the message (e.g. "deposit", "withdraw", etc.)
        bytes label;
    }

    /// @notice The wrapped sequencer address with special permissions.
    address public immutable COORDINATOR;

    /// Settlement data (chain-specific inbox and outbox roots)
    /// @notice List of chain IDs with messages in the inbox
    uint256[] public chainIDsInbox;
    /// @notice List of chain IDs with messages in the outbox
    uint256[] public chainIDsOutbox;
    /// @notice Mapping of chain ID to inbox root
    mapping(uint256 chainId => bytes32 inboxRoot) public inboxRootPerChain;
    /// @notice Mapping of chain ID to outbox root
    mapping(uint256 chainId => bytes32 outboxRoot) public outboxRootPerChain;

    /// @notice Inbox messages (to this chain)
    mapping(bytes32 key => bytes message) public inbox;
    /// @notice Outbox messages (from this chain)
    mapping(bytes32 key => bytes message) public outbox;
    /// @notice Created keys (to avoid overwrites)
    mapping(bytes32 key => bool used) public createdKeys;
    /// @notice Keys that have been already used in operations (to avoid replays)
    mapping(bytes32 key => bool used) public usedKeys;

    /// @notice Keccak256 hash of the message header fields
    function getKey(
        uint256 srcChainID,
        uint256 destChainID,
        address sender,
        address receiver,
        uint256 sessionId,
        bytes calldata label
    ) public pure returns (bytes32 key) {
        key = keccak256(
            abi.encodePacked(srcChainID, destChainID, sender, receiver, sessionId, label)
        );
    }

    function putInbox(
        uint256 srcChainID,
        address sender,
        address receiver,
        uint256 sessionId,
        bytes calldata label,
        bytes calldata data
    ) external onlyCoordinator {
        // Generate message key
        bytes32 key = getKey(
            srcChainID,  // message origin
            block.chainid,  // this chain is the destination chain
            sender, receiver,
            sessionId,
            label
        );

        // If key already exists, revert
        if (createdKeys[key]) {
            revert("Key already exists");
        }

        // Store message
        inbox[key] = data;
        createdKeys[key] = true;
    }

    function putOutbox(
        uint256 destChainID,
        address sender,
        address receiver,
        uint256 sessionId,
        bytes calldata label,
        bytes calldata data
    ) external onlyCoordinator {
        // Generate message key
        bytes32 key = getKey(
            block.chainid,  // this chain is the message origin
            destChainID,  // message destination
            sender, receiver,
            sessionId,
            label
        );

        // If key already exists, revert
        if (createdKeys[key]) {
            revert("Key already exists");
        }

        // Store message
        outbox[key] = data;
        createdKeys[key] = true;
    }



    /// @notice Read a message from the inbox if it exists. If not, reverts.
    /// @dev If the message exists, the read is confirmed and the inbox root is updated.
    function read(
        uint256 srcChainID,
        address sender,
        uint256 sessionId,
        bytes calldata label
    ) external view returns (bytes memory message) {
        // Generate message key
        bytes32 key = getKey(
            srcChainID, // other chain is the sender
            block.chainid, // this chain is receiver
            sender,
            msg.sender,
            sessionId,
            label
        );

        // If the message does not exist, revert
        if (!createdKeys[key]) {
            revert MessageNotFound();
        }

        // If the message has already been used, revert
        if (usedKeys[key]) {
            revert MessageAlreadyUsed();
        }

        // Message is valid to be read.

        // Mark as used
        usedKeys[key] = true;

        // Get message data
        bytes memory data = inbox[key];

        // Update the chain-specific inbox root
        if (inboxRootPerChain[srcChainID] == bytes32(0)) {
            chainIDsInbox.push(srcChainID);
        }
        inboxRootPerChain[srcChainID] = keccak256(
            abi.encode(inboxRootPerChain[srcChainID], key, data)
        );

        // Return message data
        return data;
    }

    /// @notice Writes a message to another chain.
    /// @dev This function can be called by other contracts in this chain.
    /// @dev For the staged mailbox, any message content is pre-populated by the Coordinator.
    /// @dev Thus, we gotta confirm that this message was really pre-populated, else it reverts.
    /// @dev Also, if the message exists and wasn't yet used, it marks it as used and updates the outbox root.
    function write(
        uint256 destChainID,
        address receiver,
        uint256 sessionId,
        bytes calldata label,
        bytes calldata data
    ) external {
        // Generate message key
        bytes32 key = getKey(
            block.chainid,  // this chain is the message sender
            destChainID, // chain destination
            msg.sender,
            receiver,
            sessionId,
            label
        );

        // If the message does not exist, revert
        if (!createdKeys[key]) {
            revert MessageNotFound();
        }
        // If the message has already been used, revert
        if (usedKeys[key]) {
            revert MessageAlreadyUsed();
        }
        // If data doesn't match the pre-populated message data, revert
        if (keccak256(outbox[key]) != keccak256(data)) {
            revert MessageDataMismatch();
        }

        // Written operation is safe and valid.

        // Mark as used
        usedKeys[key] = true;

        // Get message data
        bytes memory data = outbox[key];

        // Update the chain-specific outbox root
        if (outboxRootPerChain[destChainID] == bytes32(0)) {
            chainIDsOutbox.push(destChainID);
        }
        outboxRootPerChain[destChainID] = keccak256(
            abi.encode(outboxRootPerChain[destChainID], key, data)
        );
    }
}
```

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
Once the `safe_execute` tx is submitted to the ER, it will be re-executed with the ER's current state, and it will only succeed if the written messages are the same as the ones simulated (pre-populated in the StagedMailbox).
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

TODO
