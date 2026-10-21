# Contributing to soroauth

## Overview

## Your first contribution, in ten steps

The rest of this document is long because it carries the reasoning behind each
rule. This checklist is the short path to a green pull request; each step links
the section that explains it.

- [ ] **1. Pick an issue and get assigned.** Comment on it and wait. Complexity
      and area labels are what work is chosen from — see
      [Picking up an issue](#picking-up-an-issue).
- [ ] **2. Clone and run the suite before changing anything**, so you know a
      later failure is yours: `git clone … && cd soroauth-go && go test ./...`.
      Go 1.26.3 or later is the only requirement for the library and CLI — see
      [Setup](#setup).
- [ ] **3. Branch.** Name it after the work, not the issue number alone:
      `docs/161-onboarding-checklist`, `feat/entry-diff`.
- [ ] **4. Write the test first where you can.** A test that cannot fail reads
      as evidence and is worse than none; if you expect a failure, assert
      _which_ failure — see [What a change needs](#what-a-change-needs).
- [ ] **5. Cite anything you claim about the protocol.** A doc comment, README
      line or commit body that states host behaviour needs the CAP, the
      `rs-soroban-env` file and function, or the SDK file and line. Not memory.
- [ ] **6. Never hand-edit a golden vector.** If your change moves the bytes,
      regenerate with `make vectors` and say in the commit body which CAP
      requires the move — see [Golden vectors](#golden-vectors).
- [ ] **7. Run `make`** — that is `fmt`, `vet` and `test`. If you touched any
      Markdown, also run `npx prettier --write '**/*.md'`, because the build
      checks it. `golangci-lint run ./...` is a local gate worth running too —
      see [Before you open a pull request](#before-you-open-a-pull-request) and
      [Linting](#linting).
- [ ] **8. Commit in conventional form**, lowercase and imperative, one logical
      unit per commit, with the command you ran and its real output in the body
      — see [Commit format](#commit-format).
- [ ] **9. Open the pull request** with one `Closes #<n>` line per issue it
      resolves, what you changed, and how you verified it. A space-separated
      list (`Closes #1 #2`) links only the first issue, and a closing line added
      after the pull request is merged closes nothing, so write them before the
      merge — otherwise an issue the work delivered stays open. Two checks gate
      the merge: `vet and test` and `golden vectors are reproducible`.
- [ ] **10. If the work turned out larger than its complexity label**, say so on
      the issue rather than absorbing it silently.

One thing that is not on the list: do not report a signature-correctness or
key-handling bug in a public issue or pull request. See [Security](#security).

## Picking up an issue

Issues carry a complexity label and an area label, and those two are how work is
chosen; complexity is also what an issue's Stellar Wave points are counted from.
What each label means, who applies it, and who has the final say on complexity
is documented in [docs/labels.md](docs/labels.md).

## Setup

```sh
git clone https://github.com/soroauth/soroauth-go
cd soroauth-go
go test ./...
```

That is the whole setup for the library and CLI. Go 1.26.3 or later.

The pinned `github.com/stellar/go-stellar-sdk` version and the policy for moving
it live in [docs/sdk-support.md](docs/sdk-support.md).

Two optional pieces need more:

- **Regenerating golden vectors** needs Node (>= 22.12.0, what
  `@stellar/stellar-sdk@17.1.0` declares).
- **Running the e2e tests** needs Rust 1.93.0 (pinned in
  `e2e/contracts/rust-toolchain.toml`, rustup will fetch it) and `stellar-cli`
  28.0.0. See [the fixture contract quickstart](docs/fixture-quickstart.md) for
  a step-by-step build and deploy guide from a clean checkout.
- **Working on the wallet SDK adapter** (`adapters/walletsdk`) needs nothing
  extra, but it is a module of its own with its own test loop; see
  [The nested adapter module](#the-nested-adapter-module).
- **Running the Python parity harness** needs Python 3.10+ and the pinned SDK in
  `testdata/parity-python/requirements.txt`; `make parity` creates a venv and
  installs it. The differential fuzzing harness reuses the same venv and SDK:
  `make differential`.
- **Working on the WebAssembly core or the TypeScript wrapper** needs Node (the
  same 22+ the rest of the tooling uses). `make wasm-check` builds the module
  and proves it byte-identical to the golden vectors; `make ts-test` typechecks
  and tests the wrapper package.

## Make targets

The Makefile wraps the common tasks, so the commands below exist in one place
rather than across several documents. Run `make help` for the list.

| Target               | What it runs                                                                                          |
| -------------------- | ----------------------------------------------------------------------------------------------------- |
| `make` (default)     | `fmt`, `vet` and `test`                                                                               |
| `make fmt`           | fails if `gofmt -l .` reports anything                                                                |
| `make vet`           | `go vet ./...`                                                                                        |
| `make test`          | `go test ./...`                                                                                       |
| `make coverage`      | measures test coverage and enforces the per-package floors in `testdata/coverage/floors.json`         |
| `make soak`          | runs the long-running batch signing soak (race detector on, `SOAKTIME`, default 2m)                   |
| `make build`         | builds the CLI to `bin/soroauth`                                                                      |
| `make vectors`       | `cd testdata/gen && npm ci && node gen.mjs && node gen-passkey.mjs`                                   |
| `make vectors-check` | regenerates the vectors and fails if the committed files changed                                      |
| `make demo-check`    | checks the browser demo's logic against the pinned SDK (`node examples/browser-passkey/app.test.mjs`) |
| `make e2e`           | builds the test contract with `stellar-cli` and runs `go test -tags e2e -v ./e2e/...`                 |
| `make parity`        | installs the pinned Python SDK into `.venv-parity` and runs the parity harness and its tests          |
| `make parity-rust`   | runs the Rust stellar-xdr parity harness and its tests (needs Rust 1.93.0)                            |
| `make differential`  | regenerates the differential fuzzing corpus and checks it with the Go, JS and Python implementations  |
| `make wasm`          | builds the js/wasm signing core to `wasm/dist/`                                                       |
| `make wasm-check`    | builds the wasm core and replays every golden vector through it                                       |
| `make ts-test`       | typechecks and tests the `@soroauth/wasm` TypeScript package                                          |
| `make clean`         | removes `bin/`, the wasm build output and the parity venv                                             |

No target hides a failure. `make fmt` exits non-zero when a file needs
formatting instead of printing a warning, `make vectors-check` exits non-zero
when regeneration changes a committed vector, and `make e2e` refuses to run
without `stellar-cli` rather than failing later with an obscure test error.

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