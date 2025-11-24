
<p><img src="https://framerusercontent.com/images/9FedKxMYLZKR9fxBCYj90z78.png?scale-down-to=512&width=893&height=363" alt="SSV Network"></p>
<a href="https://discord.com/invite/ssvnetworkofficial"><img src="https://img.shields.io/badge/discord-%23ssvlabs-8A2BE2.svg" alt="Discord" /></a>


# ✨ Compose Specification

This repository hosts the canonical specification for
Compose—a network of rollups that enjoys synchronous, atomic composability through the
shared publisher architecture.
Such a feature is achieved by two mechanisms:
- a simple two-phase commit protocol that provides coordination
on the inclusion of a cross-chain transaction.
- a synchronous settlement pipeline, which finalizes all chains
simultaneously in L1 with a single ZK proof.


## 📖 Reading Guide

To read about the protocol in detail, please check:
1. [Synchronous Composability Protocol (SCP)](./synchronous_composability_protocol.md):
the fundamental building block that provides coordination
on a single cross-chain transaction inclusion.
2. [Superblock Construction Protocol (SBCP) v2](./superblock_construction_protocol_v2.md):
the orchestration layer that manages multiple SCP instances,
block construction, and defines the triggering and input for the settlement pipeline.
3. [Settlement Layer](./settlement_layer.md):
explains the settlement pipeline of Compose,
picturing the recursive ZK programs architecture which
outputs a single ZK proof about the state progress of the entire chain.

## 📝 Minimal Spec Lib Implementation

This repo contains a [minimal implementation](./compose/README.md) of the core logic of the proposed protocols.

## ⚙️ Rollup Integration

Compose’s proposed modules do not fully specify every layer of a rollup.
This is intentional: we provide minimal integrable modules,
so each rollup retains sovereignty over choices like data availability logic,
local transaction priority ordering, batching logic, and related pipeline details.
As a default, we use a modified [OP](https://docs.optimism.io/concepts/stack/getting-started) + [OP‑Succinct](https://github.com/succinctlabs/op-succinct/tree/main) stack,
currently supporting the [Isthmus hard fork](https://docs.optimism.io/concepts/stack/network-upgrades).

Read our [tutorial](https://github.com/compose-network/contracts/blob/develop/L1-settlement/README.md) for more details on rollup migration.

## 💡 Contributing

We welcome community contributions!

Please open an issue with a detailed description or
fork the repo, add a branch with your changes, and open a PR.

## 📜 License

This repository is distributed under [GPL-3.0](LICENSE).
