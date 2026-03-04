# Compose

Rust implementation of the Compose protocol specification library.

## Crates

| Crate                | Description                                                     |
|----------------------|-----------------------------------------------------------------|
| `compose-spec`       | Core types: `ChainId`, `InstanceId`, `XtRequest`, etc.          |
| `compose-spec-scp`   | 2-phase commit protocol (publisher + sequencer instances)       |
| `compose-spec-sbcp`  | Superblock construction protocol (period/settlement management) |
| `compose-spec-proto` | Protobuf wire format (hand-written prost structs)               |

## Building

```bash
cargo build --workspace
```

## Testing

```bash
cargo test --workspace
```

## Linting

```bash
cargo clippy --workspace --all-targets -- -D warnings
cargo fmt --all -- --check
```
