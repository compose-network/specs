
# Universal Shared Bridge for OP Chains  

The Universal Shared Bridge for OP Chains enables seamless asset transfers between Ethereum (L1) and any supporting OP L2, as well as directly between OP L2s. It leverages canonical custody on any chain, minting of "ComposeableERC20" (CET) tokens to represent bridged assets on L2s, and supports secure burn/mint logic for moving assets between L2s. All mint/burn operations are exclusively handled by the bridge contracts, ensuring safety. The bridge employs OP-Succinct proof mechanisms for L1 verifications and introduces efficient message-based transfers for L2↔L2 bridging, with formal post-transfer settlement. 

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
12. **TODO:** Allow token owner to have the bridge mint native token on specified conditions.


___

# L2↔L2 Bridge --- Sessioned Mailbox Flow


### Entities & Contracts

-  **ComposableERC20**
    An ERC20 wrapper native to the bridge.

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

-   **Supported Token types**

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

    **TODO** More details?

------------------------------------------------------------------------

##  ComposeableERC20

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
        address remoteAsset;
        /// @notice Name of the token
        string name;
        /// @notice Symbol of the token
        string symbol;
        /// @notice Decimals of the token
        uint8 decimals;
    }
```

### Minting CET on the fly

The bridge can mint CETs on demand via a factory

```solidity
interface ICETFactory {
    function predictAddress(address l1Asset) external view returns (address predicted);
    function deployIfAbsent(
        address l1Asset,
        uint8 decimals,
        string calldata name,
        string calldata symbol,
        address bridge
    ) external returns (address deployed);
}

ICETFactory public cetFactory;

function computeCETAddress(address remoteAsset) internal view returns (address) {
    return cetFactory.predictAddress(remoteAsset);
}

function ensureCETAndMint(
    address remoteAsset,
    string calldata name,
    string calldata symbol,
    uint8 decimals,
    address to,
    uint256 amount
) internal returns (address cet) {
    // 1) Predict deterministic address
    address predicted = computeCETAddress(remoteAsset);

    // 2) Deploy if missing (CREATE3-based factory)
    cet = cetFactory.deployIfAbsent(remoteAsset, decimals, name, symbol, address(this));
    require(cet == predicted, "CET address mismatch");

    // 3) Mint via bridge-only path
    IToken(cet).mint(to, amount);
    return cet;
}
```

-------------

###  Mailbox

Mailbox is a container of `Messages` divided into 2 boxes: 
- `Inbox` that has messages consumed by `Read()` function
- `Outbox` that has messages pushed into by `Write()` function.


``` solidity
  struct Message {
    MessageHeader header,
    bytes payload
  }

// Header for message. Its hash can serve as the message Identifier.
    struct MessageHeader {
        uint256 sessionId; // 16 bytes of version + 240 bytes of value chosen by the user
        uint256 chainSrc;  // chain ID where the message was sourced from
        uint256 chainDest; // chain ID of target destination
        address sender;    // The address on `chainSrc` initiating the message 
        address receiver;  // The address on `chainDest` receiving the message
        string label;      // Helps decipher the payload.
    }    

interface IMailbox {
    // `sender` writes to the OUTBOX a message to be relayed to `reciever` on `chainDest`
    function write(
        Message calldata message
    ) external;

    // `receiver` reads from the INBOX a message relayed by `sender` from `srcChain`
    function read(
        MessageHeader calldata header
    ) external returns (bytes memory);
}
```



#### SessionID

SessionID is a 240 bits random value that MUST NOT be reused across MessageHeaders with otherwise similar values.
However, every message in a cross-chain exchange mapping to a single atomic operation must carry the same SessionID.

The first 16 bytes serve as version. Currently the only canonical version is 0.

#### Replay Protection

The `Read` function will return an error if it will be invoked more than once with the same `MessageHeader`


------------------------------------------------------------------------


###  Payload Schema

The bridge supports 2 payload types that have the following labels on the *Mailbox*:

- `SEND`
- `ACK`

#### SEND Payload

All `SEND` payloads use a single canonical ABI encoding:

    abi.encode(
      uint256 remoteChainID  // The native chain of the transferred asset
      address remoteAsset,   // canonical asset address
      uint256 amount         // amount
    )


#### ACK payload:

    Just empty `{}`

------------------------------------------------------------------------


### Source L2: bridge entrypoints

It is important to note that 

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
    uint256 chainDest,         // Destination ChainID
    address cetTokenSrc,       // CET on source L2
    address remoteAssetAddress // original 
    uint256 amount,
    address receiver,          // address on destination L2
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
// bridges all non CET ERC-20 tokens
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

    bytes memory payload = abi.encode(block.chainid, tokenSrc, amount);

    mailbox.write(chainDest, receiver, sessionId, "SEND", payload);
    // performs a mailbox read for an "ACK" labeled message.
    checkAck(chainDest, sessionID)

    emit MailboxWrite(chainDest, receiver, sessionId, "SEND");

    bytes32 messageId = keccak256(abi.encodePacked(chainDest, receiver, sessionId, "SEND"));
    emit TokensSendQueued(chainDest, sender, receiver, tokenSrc, amount, sessionId, messageId);
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

    address remoteAsset = ICET(cetTokenSrc).remoteAsset(); 
    uint256 remoteChainID = ICET(cetTokenSrc).remoteChainID();


    bytes memory payload = abi.encode(remoteChainID, remoteAsset, amount);

    mailbox.write(chainDest, receiver, sessionId, "SEND", payload);
    checkAck(chainDest, sessionID)

    emit MailboxWrite(chainDest, receiver, sessionId, "SEND");

    bytes32 messageId = keccak256(abi.encodePacked(chainDest, receiver, sessionId, "SEND"));
    emit TokensSendQueued(chainDest, sender, receiver, cetTokenSrc, amount, sessionId, messageId);
}
```

------------------------------------------------------------------------

###  Destination L2 --- Claim Flow
``` solidity
function receiveTokens(
    MessageHeader msgHeader
    // the following parameters are only needed if the proper CET token wasn't deployed
    // TODO: should they be part of the message?
    string calldata name,
    string calldata symbol,
    uint8 decimals
) external returns (address token, uint256 amount) {
    require(msg.sender == msgHeader.receiver, "Only receiver can claim");
    require(msgHeader.chainDest == block.chainid, "Wrong destination chain");
    require(msgHeader.label == "SEND", "Must read a SEND message")

    // 1) Read and consume the SEND message
    bytes memory m = mailbox.read(MessageHeader(sessionIDchainSrc, receiver, sessionId, "SEND");
    if (m.length == 0) revert("No SEND message");

    uint256 rmoteChainID;
    address remoteAsset;

    (remoteChainID, remoteAsset, amount) =
        abi.decode(m, (uint256, address, uint256));


    // 2) RELEASE if native token is hosted & escrowed here, else MINT BCT
    if remoteChainID == block.chainid && IERC20(remoteAddress).balanceOf(address(this)) >= amount) {
        // Release escrowed native tokens
        require(IERC20(native).transfer(receiver, amount), "Native release failed");
        token = native;
    } else {
        // Mint deterministic CET on this chain
        token = ensureCETAndMint(remoteAddress, name, symbol, decimals, msgHeader.receiver, amount)
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

###  End-to-End Lifecycle

1.  Sender calls `bridgeERC20To` or `bridgeCETTo` with sessionId.
2.  Source bridge locks/burns + writes `"SEND"` with L1 asset address.
3.  Receiver calls `receiveTokens`.
4.  Destination bridge reads `"SEND"`, computes CET address from
    `remoteAsset`, mints, writes `"ACK SEND"`
5.  ACK is visible for source monitoring.

------------------------------------------------------------------------

## L1 <-> L2 Bridge For native rollups.

We need to utilize the current OP-contracts with minimal changes. Namely the `StandardBridge.sol`, `CrossDomainMessenger.sol`, and thei L1/L2 couterparts are neccessary. 

Currently an OP rollup manage the L1<->L2 bridge via `OptimismPortal2` contract. This utilizes an `ETHLockbox` contract that locks all deposited ETH. Each native rollups using the universal bridge will deploy a `ComposePortal` (similar to `OptimismPortal)  that will use a shared `ETHLockBox` and an `ERC20LockBox`. The sharing of a single `LockBox` will ensure that funds deposited on any chain can be withdrawn via another chain.

The `OptimismPortal2` generate `TransactionDeposited` events, that are captured on OP-GETH and are relayed to the standard OP-Bridge contracts. The `StandardBridge:finalizeBridgeERC20` call must be changed so it will mint `ComposableERC20s`.

## TODO: Can we do L1<->L2 bridge for external rollups



### ETH
Not w.o having liquidity available on the external rollup.

### ERC-20

Currently Op-Succinct Sequencers pick up `TransactionDeposited` Events to relay bridge messages.
They check the address of the contract that originated the event. And perform a ZK proof that the event was included in the `recieptsRoot`

In the case of an external interop sequencer, a malicious wrapped sequencer may send a non backed log. This won't be ZK proven but it will still become part of the external rollup canonical state. 
The result will be that the connection with the Universal Shared Bridge will be severed. <TODO: Ensure this is true>


