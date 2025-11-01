# Compose Spec Code


To generate the Go proto files:

```bash
cd ./compose
protoc --proto_path=proto --go_out=proto --go_opt=paths=source_relative proto/protocol_messages.proto
```
