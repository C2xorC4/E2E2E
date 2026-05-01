# E2E2E — End-to-End-(to-End)

A chat application **demonstration** that exposes a structural deception in many "end-to-end encrypted" messaging products: the centralized coordination server is itself an *end*, with full visibility into plaintext messages.

The architecture is **deliberately** designed this way. Cryptographic primitives are correctly implemented (X25519 ECDH, XChaCha20-Poly1305 AEAD, BLAKE2b, Argon2id) and transport is properly encrypted between each client and the server — but the server decrypts on receipt, stores plaintext in PostgreSQL with full-text search, and re-encrypts per-recipient device for outbound delivery. From the user's perspective the application looks and behaves like a sincere E2E messenger; from the operator's perspective it is a complete intelligence platform.

This is intentional. It is the point.

## Why this exists

To make the structural-deception class of "E2E" messaging concretely visible — not as an abstract critique but as a working artifact users can install, inspect, and operate. Real cryptographic primitives correctly implemented are *not sufficient*: the trust model dominates the security guarantees, and "end-to-end" is a marketing claim that requires a verifiable architectural property to back it up.

Intended audiences:

- Security architects evaluating "E2E" claims in vendor products
- Red teams demonstrating trust-model gaps to clients during engagements
- Blue teams developing detection / verification methodologies for messenger architectures
- Educators teaching distributed-systems trust models and threat modeling
- Researchers analyzing the gap between marketing claims and actual security guarantees

## What this is *not*

- **Not** a real secure-messaging product. Do not use this for any communication where confidentiality matters. The deception is the demonstration; deploying it as a real service is the harm.
- **Not** a vulnerability disclosure of any specific commercial messenger. The pattern documented here is widespread across the industry; this project is a generic structural demonstration, not an attack against a specific target.
- **Not** an attempt to disparage E2E as a category. Several legitimate messengers (Signal, recent WhatsApp configurations, etc.) provide architectures where the server cannot decrypt content. This project demonstrates the *failure mode* that distinguishes the sincere implementations from the marketing-claim ones.

## Architecture

See [`Design.md`](./Design.md) for the full architectural specification, including:

- Cryptographic primitive selection and rationale
- Per-user identity keys + per-device keys + server-held transport keys
- Server-side decrypt + plaintext storage mechanism (the deceptive layer)
- PostgreSQL schema with `tsvector` full-text search on plaintext messages
- WebSocket message routing with goroutine-per-connection model
- Multi-platform client + multi-device fanout
- Authentication, session, and key-storage details

## Repository structure

| Path | Contents |
|---|---|
| `client/` | Flutter cross-platform client (Windows / Linux / macOS / Android / iOS / Web) |
| `linux-client/` | Standalone Go TUI client for Linux — operationally simpler, useful for stress testing and demonstration scripting |
| `server/` | Go server: HTTP / WebSocket handlers, auth, crypto, DB, rate limiting, admin module |
| `database/` | PostgreSQL schema + migrations |
| `Design.md` | Full architectural specification |

## Status

Working prototype. Server has rate limiting, admin endpoints, and concurrency-tested message routing. Flutter client implements the full user-facing surface. Linux Go TUI client has been used for concurrency stress testing.

Suitable for demonstration, education, and authorized security-research use. Not suitable as a production messenger.

## Authorized-use scope

This tool is for authorized security research, education, and demonstration only.

**Do not** deploy as a real messaging service.
**Do not** use to deceive end-users about the security posture of their communications.
**Do not** represent the application's "encryption" claim as actual end-to-end without disclosing the architectural trust model.

The repository is published to make the structural deception inspectable; it is not published as a permission to exploit users who don't understand the architecture.

## Why "E2E2E"

End-to-End-(to-End). The middle "End" is the server — the silent third party that the marketing claim doesn't acknowledge. Reading the name aloud is itself the indictment.

## License

License pending. Consult the maintainer before use beyond inspection / education.
