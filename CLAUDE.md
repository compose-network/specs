# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

This repository contains the canonical specification for **Compose**—a network of rollups with synchronous, atomic composability through shared publisher architecture. The codebase provides:

1. Protocol documentation (markdown specs)
2. Minimal Go implementation of core protocol logic in `compose/`

## Build & Test Commands

```bash
# Run all tests from compose directory
cd compose && go test ./...

# Run tests for a specific package
cd compose && go test ./scp/...
cd compose && go test ./sbcp/...

# Generate protobuf files (requires protoc installed)
cd compose && protoc --proto_path=proto --go_out=proto --go_opt=paths=source_relative proto/protocol_messages.proto
```

## Architecture

### Two-Layer Protocol Design

The Compose protocol operates with two complementary layers:

1. **SCP (Synchronous Composability Protocol)** - `compose/scp/`
   - Handles coordination for individual cross-chain transactions
   - Two-phase commit protocol: StartInstance → Vote collection → Decided
   - `PublisherInstance`: Coordinates votes from participating chains
   - `SequencerInstance`: Executes mailbox-aware simulation, handles read misses

2. **SBCP (Superblock Construction Protocol)** - `compose/sbcp/`
   - Orchestrates multiple SCP instances across periods
   - Manages block construction and settlement pipeline triggering
   - `Publisher`: Period management, proof collection, L1 publishing
   - `Sequencer`: Block building policy, settlement coordination

### Key Concepts

- **Period**: 10 Ethereum epochs (~64 minutes), during which one superblock is constructed
- **Superblock**: Aggregated state from all chains, proven with a single ZK proof
- **Instance**: A single cross-chain transaction coordination session (SCP level)
- **Mailbox**: Cross-chain message passing mechanism with read-miss handling

### Interface Dependencies

Both protocols use dependency injection for external systems:

**SCP:**
- `ExecutionEngine`: VM simulation with mailbox tracing
- `PublisherNetwork`/`SequencerNetwork`: Message passing

**SBCP:**
- `PublisherProver`/`SequencerProver`: ZK proof generation
- `PublisherMessenger`/`SequencerMessenger`: Protocol messages
- `L1`: Settlement layer interaction

### Core Types (`compose/compose.go`)

- `ChainID`, `PeriodID`, `SuperblockNumber`, `SequenceNumber`
- `XTRequest`: Cross-chain transaction request containing transactions per chain
- `Instance`: SCP instance metadata (ID, period, sequence, request)
- `DecisionState`: Pending/Accepted/Rejected

## Protocol Flow Summary

1. **Period Start**: Publisher broadcasts `StartPeriod`, sequencers prepare for new superblock
2. **XTRequest**: User submits cross-chain tx → forwarded to publisher → `StartInstance`
3. **SCP Instance**: Sequencers simulate, exchange mailbox messages, vote
4. **Decision**: All-true votes → Accept; any false/timeout → Reject
5. **Settlement**: Period ends → sequencers generate proofs → publisher aggregates → L1 publish
