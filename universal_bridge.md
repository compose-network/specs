
# Universal Shared Bridge for OP Chains  
*(Validity-Proof / OP-Succinct Compatible; Direct L2↔L2 “mint-on-message” fast path)*

##  Objectives & Authoritative Requirements

1. **L1 Custody:** Ethereum (L1) is the main escrow (but not the only one) for **canonical ERC-20s and ETH**.  
2. **L1→L2 Representation:** Depositing an L1 ERC-20 to any OP L2 mints a **ComposeableERC20 (CET)** on that L2.  
3. **L2↔L2 Bridging:** The bridge supports moving both **native ERC-20** and **CET** between OP L2s. 
4. **L2↔L2 (ERC-20):** If the source asset is a **native ERC-20** on L2, it is **Locked** on the source L2 and a **CET is Minted** on the destination L2.  
5. **L2↔L2 (CET):** If the source asset is a **CET**, it is **Burned** on the source L2 and **Minted** on the destination L2.  
6. **L2→L1 Redemption:** Any CET can always be redeemed to **unlock the collateral on L1**.  
7. **Proof System:** The design **builds on OP-Succinct** (validity proofs). L1 verifications rely on **per-L2 finalized post roots**
8. **Mint Authority / Safety:** Only **bridge contracts** may mint or burn **CET**; no external mint paths.  
9. **Exit Logic (proof-based paths):** Use the **same exit logic as OP-Succinct**: prove claims against a chain’s **post root**, then an **MPT/storage proof** to the chain’s **Outbox/Exit root**, then a **Merkle inclusion** for the exit record.  
10. **Replay Protection:** Messages can be consumed only once. Any replay will be ignored.  
11. **Inter-L2 Fast Path:** For **L2↔L2** transfers, the destination **mints on receipt of a bridge message** (no proof verification at claim time). **Later settlement** is done simultaneously via aggregated proofs (out of scope here).
12. **TBD** Allow token owner to have the bridge mint native token on specified conditions.

---

##  Introducing the ComposeableERC20

Inspired by `OptimismSuperChainERC20`, this is an ERC7802 compliant token for cross-chain transfers.
The code snippet below describe the main functionality.

```solidity
abstract contract ComposeableERC20 is ERC20, IERC7802 {
    /// @param _to     Address to mint tokens to.
    /// @param _amount Amount of tokens to mint.
    function crosschainMint(address _to, uint256 _amount) external {
        if (msg.sender != COMPOSE_TOKEN_BRIDGE) revert Unauthorized();

        _mint(_to, _amount);

        emit CrosschainMint(_to, _amount, msg.sender);
    }

    /// @param _from   Address to burn tokens from.
    /// @param _amount Amount of tokens to burn.
    function crosschainBurn(address _from, uint256 _amount) external {
        if (msg.sender != COMPOSE_TOKEN_BRIDGE) revert Unauthorized();

        _burn(_from, _amount);

        emit CrosschainBurn(_from, _amount, msg.sender);
    }
    
       /// @notice Storage struct for the BridgedComposeTokenERC20 metadata.
    struct BridgedComposeTokenERC20Metadata {
        /// @notice The ChainID where this token was originally minted.
        uint256 remoteChainID
        /// @notice Address of the corresponding version of this token on the remote chain.
        address remoteToken;
        /// @notice Name of the token
        string name;
        /// @notice Symbol of the token
        string symbol;
        /// @notice Decimals of the token
        uint8 decimals;
    }
```

___

# L2↔L2 Bridge --- Sessioned Mailbox Flow


### Entities & Contracts

-   **Bridge (per L2)**
    Handles: locking native ERC-20, burning CET, mailbox write/read, and
    minting (via token's `onlyBridge` gate when called from the bridge
    context).

-   **Mailbox (per L2)**
    Minimal append-only message bus with read-once semantics per
    `(chainSrc, recipient, sessionId, label)`.

    -   `write(chainId, account, sessionId, label, payload)`\
    -   `read(chainId, account, sessionId, label) → bytes`
        (consumes/marks delivered)

-   **Token types**

    -   **Native ERC-20** on an L2 (not minted by bridge).
    -   **CET** (ComposeableERC20Token) --- canonical L2 representation
        of L1/L2 asset.
        -   `mint`/`burn` are **restricted** to `msg.sender == Bridge`.

-   **Deterministic CET Addresses (Superchain-style)**
    Each CET contract is deployed **at the same address across all OP
    L2s**, deterministically derived from the L1 canonical asset address
    (using CREATE2/CREATE3).

    -   This eliminates the need for a per-chain registry.
    -   Mailbox payloads only need to carry the **`remoteToken` address**.
    -   On the destination chain, the bridge deterministically computes
        the CET address and mints to the receiver.

------------------------------------------------------------------------

###  Message Schema (Mailbox payload)

All `SEND` payloads use a single canonical ABI encoding:

    abi.encode(
      address sender,        // original EOA or contract on source L2
      address receiver,      // recipient on destination L2
      address remoteAsset,       // canonical L1 asset address
      uint256 amount         // amount (decimals same as its CET)
    )

Notes: - On **destination L2**, the bridge computes the **CET address
deterministically** from `remoteAsset`
- This ensures the CET address is consistent across all L2s, without a
registry.

ACK payload:

    abi.encode("OK")

Labels: - Outbound: `"SEND"` - Return ACK: `"ACK SEND"`

------------------------------------------------------------------------


### Source L2: bridge entrypoints (sessionized)


``` solidity
/// Lock native ERC-20 on source and send SEND message
function bridgeERC20To(
    uint256 chainDest,      // Destination ChainID
    address tokenSrc,       // native ERC-20 on source L2
    uint256 amount,
    address receiver,      // address on destination L2
    uint256 sessionId
) external;

/// Burn CET on source and send SEND message
function bridgeCETTo(
    uint256 chainDest,      // Destination ChainID
    address cetTokenSrc,    // CET on source L2
    uint256 amount,
    address receiver,       // address on destination L2
    uint256 sessionId
) external;
```

### Destination L2: recipient claim

``` solidity
/// Process funds reception on the destination chain
/// @param chainSrc source chain identifier the funds are sent from
/// @param chainDest dest chain identifier the funds are sent to
/// @param sender address of the sender of the funds
/// @param receiver address of the receiver of the funds
/// @param sessionId identifier of the user session
/// @return token address of the token that was transferred
/// @return amount amount of tokens transferred
function receiveTokens(
    uint256 chainSrc,
    uint256 chainDest,
    address sender,
    address receiver,
    uint256 sessionId
) external returns (address token, uint256 amount);
```

> Note: receiveTokens` sits **on the Bridge contract**
> (so that when it calls `CrossChainMint`, the token sees
> `msg.sender == Bridge` and respects the `onlyBridge` mint gate), while
> still requiring `msg.sender == receiver` to enforce "only receiver can
> claim".

------------------------------------------------------------------------

### Events

``` solidity
event TokensSendQueued(
    uint256 indexed chainDest,
    address indexed sender,
    address indexed receiver,
    address remoteAsset,
    uint256 amount,
    uint256 sessionId,
    bytes32 messageId
);

event TokensLocked(address indexed token, address indexed sender, uint256 amount);
event CETBurned(address indexed token, address indexed sender, uint256 amount);
event MailboxWrite(uint256 indexed chainId, address indexed account, uint256 indexed sessionId, string label);
event TokensReceived(address indexed token, uint256 amount);
event MailboxAckWrite(uint256 indexed chainId, address indexed account, uint256 indexed sessionId, string label);
```

------------------------------------------------------------------------

### Source L2 --- Execution Flow & Pseudocode
###  `bridgeERC20To`

``` solidity
function bridgeERC20To(
    uint256 chainDest,
    address tokenSrc,
    uint256 amount,
    address receiver,
    uint256 sessionId
) external {
    address sender = msg.sender;

    IERC20(tokenSrc).transferFrom(sender, address(this), amount);
    emit TokensLocked(tokenSrc, sender, amount);

    address remoteAsset = ERC20Metadata(tokenSrc).remoteSource(); // canonical L1 address

    bytes memory payload = abi.encode(sender, receiver, remoteAsset, amount);

    mailbox.write(chainDest, receiver, sessionId, "SEND", payload);
    // performs a mailbox read for an "ACK" labeled message.
    checkAck(chainDest, sessionID)

    emit MailboxWrite(chainDest, receiver, sessionId, "SEND");

    bytes32 messageId = keccak256(abi.encodePacked(chainDest, receiver, sessionId, "SEND"));
    emit TokensSendQueued(chainDest, sender, receiver, remoteAsset, amount, sessionId, messageId);
}
```

###  `bridgeCETTo`

``` solidity
function bridgeCETTo(
    uint256 chainDest,
    address cetTokenSrc,
    uint256 amount,
    address receiver,
    uint256 sessionId
) external {
    address sender = msg.sender;

    ICET(cetTokenSrc).crossChainburn(sender, amount);
    emit CETBurned(cetTokenSrc, sender, amount);

    address remoteAsset = ICET(cetTokenSrc).remoteSource(); // extract canonical L1 address

    bytes memory payload = abi.encode(sender, receiver, remoteAsset, amount);

    mailbox.write(chainDest, receiver, sessionId, "SEND", payload);
    checkAck(chainDest, sessionID)

    emit MailboxWrite(chainDest, receiver, sessionId, "SEND");

    bytes32 messageId = keccak256(abi.encodePacked(chainDest, receiver, sessionId, "SEND"));
    emit TokensSendQueued(chainDest, sender, receiver, remoteAsset, amount, sessionId, messageId);
}
```

------------------------------------------------------------------------

###  Destination L2 --- Claim Flow
``` solidity
function receiveTokens(
    uint256 chainSrc,
    uint256 chainDest,
    address sender,
    address receiver,
    uint256 sessionId
) external returns (address token, uint256 amount) {
    require(msg.sender == receiver, "Only receiver can claim");
    require(chainDest == block.chainid, "Wrong destination chain");

    // 1) Read and consume the SEND message
    bytes memory m = mailbox.read(chainSrc, receiver, sessionId, "SEND");
    if (m.length == 0) revert("No SEND message");

    address readSender;
    address readReceiver;
    address remoteAsset;

    (readSender, readReceiver, remoteAsset, amount) =
        abi.decode(m, (address, address, address, uint256));

    require(readSender == sender, "Sender mismatch");
    require(readReceiver == receiver, "Receiver mismatch");

    // 2) RELEASE if native token is hosted & escrowed here, else MINT BCT
    address native = nativeTokenOnThisChain(remoteAsset);
    if (native != address(0) && IERC20(native).balanceOf(address(this)) >= amount) {
        // Release escrowed native tokens
        require(IERC20(native).transfer(receiver, amount), "Native release failed");
        token = native;
    } else {
        // Mint deterministic BCT on this chain
        token = computeBCTAddress(remoteAsset);
        ICET(token).crossChainMint(receiver, amount);
    }

    // 3) ACK back to source
    mailbox.write(chainSrc, sender, sessionId, "ACK SEND", abi.encode("OK"));

    emit TokensReceived(token, amount);
    return (token, amount);
}
```

------------------------------------------------------------------------

### Safety & Replay

-   **Mint authority:** CET enforces `onlyBridge` on `mint`/`burn`.
-   **Replay protection:** `mailbox.read(...)` must be consume-once.
-   **No registry needed:** CET address is computed deterministically
    from L1 asset address.
-   **ACK:** Ensures mailbox equivalency.

------------------------------------------------------------------------
###  Mailbox Interface

``` solidity
interface IMailbox {
    function write(
        uint256 chainId,
        address account,
        uint256 sessionId,
        string calldata label,
        bytes calldata payload
    ) external;

    function read(
        uint256 chainId,
        address account,
        uint256 sessionId,
        string calldata label
    ) external returns (bytes memory);
}
```

------------------------------------------------------------------------

###  End-to-End Lifecycle

1.  Sender calls `bridgeERC20To` or `bridgeCETTo` with sessionId.
2.  Source bridge locks/burns + writes `"SEND"` with L1 asset address.
3.  Receiver calls `receiveTokens`.
4.  Destination bridge reads `"SEND"`, computes CET address from
    `remoteAsset`, mints, writes `"ACK SEND"`
5.  ACK is visible for source monitoring.

------------------------------------------------------------------------



## Modify L1 contracts to make Shared Deposits work





### OpPortal2.sol

1. Needs to work with CET standard
2. Need to have a `LockBox` that supports ERC20s. Must always use the lockbox.


**Note regarding interop sequencers** 

Currently Op-Succinct Sequencers pick up `TransactionDeposited` Events to relay bridge messages.
They check the address of the contract that originated the event. And perform a ZK proof that the event was included in the `recieptsRoot`

In the case of an external interop sequencer, a malicious wrapped sequencer may send a non backed log. This won't be ZK proven but it will still become part of the external rollup canonical state. 
The result will be that the connection with the Universal Shared Bridge will be severed.


