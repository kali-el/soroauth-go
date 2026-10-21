# Contributing to soroauth

## Overview

`soroauth` is a Go library for building, signing, and inspecting Soroban authorization entries. This document guides contributors through setup, vector regeneration, testing, and the release process.

## Setup

1. **Install Go**: Requires Go 1.25.0 or newer (the toolchain version in `go.mod`).
2. **Install Node.js**: Required for generating golden vectors (`@stellar/stellar-sdk@17.1.0`).
3. **Clone the repository**: `git clone https://github.com/soroauth/soroauth-go.git`
4. **Install dependencies**: `go mod download`
5. **Verify the environment**:
   - `go version` should report Go 1.25.0+
   - `node --version` should report Node.js current LTS
   - `npm ls @stellar/stellar-sdk` should show `17.1.0` exactly

## Adding Recipes (Issue #408)

When adding a new recipe to the README:

1. Create a new Go file in `internal/readmesnippets/` with a `// snippet:start <name>` / `// snippet:end <name>` marker pair.
2. The code between markers must be a complete, compiling program (not a fragment).
3. Add the snippet name and source file to `readme_test.go` in the `readmeSnippets` variable.
4. Add the ```go code block to `README.md` in the appropriate location (after the existing blocks, maintaining order).
5. Run `go test -run TestReadmeSnippetsMatchTheirSource -v` to verify all blocks match.

**Golden vector rule**: Never edit a vector by hand. If a golden vector needs updating, regenerate it with `make vectors` and commit the new JSON. The CI drift check (`make vectors-check`) will fail if the Go code produces different output.

## Regenerating Golden Vectors

Vectors are generated in `testdata/vectors/` by `gen.mjs` using `@stellar/stellar-sdk@17.1.0`.

```bash
# Install dependencies
cd testdata/gen && npm ci

# Generate all vectors
node gen.mjs

# Run the drift check (compares Go output against vectors)
make vectors-check
```

**Important**: The generator refuses to run if the loaded SDK version is not exactly `17.1.0`. The `sdk` field in each generated JSON records the actual version loaded.

## Running Tests

```bash
# Run all unit tests (no network)
go test ./...

# Run the README snippet test specifically
go test -run TestReadmeSnippetsMatchTheirSource -v

# Run e2e tests (requires testnet RPC)
go test -tags e2e ./e2e/...

# Run the vet check
go vet ./...
```

## E2E Test Scenarios

E2E tests (build tag `e2e`) interact with Stellar testnet. Each scenario:

1. Funds accounts via friendbot
2. Reads the RPC URL from `SOROAUTH_RPC_URL` (default: `https://soroban-testnet.stellar.org`)
3. Verifies the RPC is reachable with a live call
4. Builds, signs, and submits a transaction
5. Polls until the transaction is done
6. Logs the transaction hash, ledger, credential arm, and explorer link
7. Appends results to `e2e/RESULTS.md`

**Scenarios**:
- **A**: Legacy arm signing accepted live
- **B**: V2 arm accepted live
- **C**: `AccountMultiSigner` accepted live
- **D**: Delegates arm accepted live
- **E**: Host rejects unregistered delegate
- **F-I**: Session keys and M-of-N delegation fixtures
- **J-Q**: Passkey signing scenarios
- **L-N**: Expiration and nonce replay scenarios

Results are stored in `e2e/RESULTS.md` with date and protocol version.

## Commit Format

Follow conventional commits, lowercase and imperative:

```
feat(authorize): add ForAddress option for delegate nodes
test(golden): add vector 9 byte-for-byte verification
docs(readme): add quickstart 20-line example
ci: add golden drift check job
chore: pin js reference SDK to 17.1.0
```

No force-pushes. Never commit secrets. Test keys are derived from public labels.

## Vector Generation Workflow

1. Run `make vectors` to regenerate `testdata/vectors/*.json`
2. Run `make vectors-check` to verify Go output matches vectors
3. If drift is detected, investigate the Go code change and update vectors if protocol-approved
4. Commit all changes (vectors, go.mod, go.sum, README.md, readme_test.go)

**Never edit a golden vector by hand** - it constitutes a wire-format regression.

## License

This project is Apache-2.0 licensed. See `LICENSE` for details.

## Security Issues

Report security issues via GitHub Security Advisories. Do not create public issues for security bugs. The scope includes signature-correctness bugs (critical), not documentation improvements or feature requests.

## Questions?

Open an issue or start a discussion in the GitHub repository.