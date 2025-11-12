
<p align="center"><img src="https://framerusercontent.com/images/9FedKxMYLZKR9fxBCYj90z78.png?scale-down-to=512&width=893&height=363" alt="SSV Network"></p>
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

To read about the protocol in detail, please check:
- [Synchronous Composability Protocol (SCP)](./synchronous_composability_protocol.md):
the fundamental building block that provides coordination
on a single cross-chain transaction inclusion.
- [Superblock Construction Protocol (SBCP)](./superblock_construction_protocol.md):
the orchestration layer that manages multiple SCP instances,
block construction, and defines the triggering and input for the settlement pipeline.
- [Settlement Layer](./settlement_layer.md):
explains the settlement pipeline of Compose,
picturing the recursive ZK programs architecture which
outputs a single ZK proof about the state progress of the entire chain.

## 📖 Reading Guide

1. Start with [SCP](./synchronous_composability_protocol.md)
for the basic understanding of how coordination and atomicity are made possible.
2. Then, continue with [SBCP](./superblock_construction_protocol.md)
to understand how multiple SCPs are managed.
3. Finish with the [settlement layer](./settlement_layer.md)
to understand how the activity performed during an SBCP period
is proven in a parallel and efficient manner, to finalize the
network synchronously.

## ⚙️ Contributing

We welcome community contributions!

Please open an issue with a detailed description or
fork the repo, add a branch with your changes, and open a PR.

## 📜 License

Repository is distributed under [GPL-3.0](LICENSE).
