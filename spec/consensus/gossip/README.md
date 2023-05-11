# README

## Organization

This specification is divided into multiple documents and should be read in the following order:

- [architecture.md](architecture.md): describes the architecture used in CometBFT and where this specification is focused;
- [crdt.md](crdt.md): explains the rationale for using a CRDT in the gossiping and defines the CRDT used, SSE;
- [sse.qnt](sse.qnt): Quint spec of our two instantiation examples of our CRDT;
- [gossip.md](gossip.md):


## Conventions

- MUST, SHOULD, MAY... are used according to RFC2119.
- [X-Y-Z-W.C]
    - X: What
        - VOC: Vocabulary
        - DEF: Definition
        - REQ: Requires
        - PROV: Provides
    - Y-Z: Who-to whom
    - W.C: Identifier.Counter

## Status

- V1 - Consolidation of work done on PR #74 as a "mergeable" PR.
- V2 - Refined type CRDT and example instantiations
- V3 - CRDT for Gossip
