# Compose Spec Code

This directory contains the Compose Specification library code.

## Modules
 
- [compose.go](./compose.go): Compose basic types.
- [proto](./proto/README.md): Protocol Buffers definitions for protocol messages.
- [scp](./scp/README.md): Synchronous Composability Protocol module.
- [sbcp](./sbcp/README.md): Superblock Construction Protocol module.

## Generating Go Proto Files

To generate the Go proto files, run:

```bash
cd ./compose
protoc --proto_path=proto --go_out=proto --go_opt=paths=source_relative proto/protocol_messages.proto
```
