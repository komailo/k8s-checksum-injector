# Repository Guidelines

## Project Structure & Module Organization
- `cmd/k8s-checksum-injector/main.go` holds the CLI entrypoint and all checksum logic; keep shared helpers close to their call sites until a second entrypoint emerges.
- `example/` contains canonical input/output manifests—use it to validate new checksum scenarios before wiring them into tests.
- `bin/` receives local builds from the Makefile; avoid committing its contents.
- `.github/workflows/ci.yml` codifies the expected build, lint, and test steps, and `Makefile` mirrors those targets for local use.

## Build, Test, and Development Commands
- `make build` (or `go build ./cmd/k8s-checksum-injector`) compiles the CLI into `bin/k8s-checksum-injector`.
- `make test` / `go test ./...` runs the Go unit-test suite against all packages.
- `make lint` executes `golangci-lint run`; address warnings before opening a PR.
- `cat example/input.yaml | go run ./cmd/k8s-checksum-injector --mode label` is a quick smoke test for manifest changes.

## Coding Style & Naming Conventions
- Target Go 1.25.1 as declared in `go.mod`; run `gofmt` (tabs + gofmt defaults) and `goimports` on every edit.
- Prefer small, composable functions and table-driven logic for YAML transforms; name helpers after the Kubernetes concepts they touch (e.g., `sanitizeKey`, `processDeploymentDoc`).
- Keep exported identifiers CamelCase, package-level constants in `CamelCase`, and avoid creating new packages until reuse is clear.

## Testing Guidelines
- Place tests beside implementation files with the `_test.go` suffix and descriptive function names like `TestProcessDeploymentDoc_ConfigMap`.
- Use table-driven tests to cover new reference types or edge cases; include fixtures inline when practical.
- Run `go test ./...` before pushing and ensure CI’s lint step stays green.

## Commit & Pull Request Guidelines
- History currently uses short imperative summaries (see `Initial commit`); keep titles ≤72 chars in the imperative, e.g., `Add annotation mode hashing`.
- Reference related issues or manifests in the body, and call out behavioral impacts.
- PRs should describe the motivation, list verification commands (tests, example runs), and note any follow-up work; add screenshots only when user-facing output changes.

## Release & Packaging
- Tag releases as `v*` to trigger `.github/workflows/release.yml`, which runs GoReleaser with Go 1.25.
- Use `make release` (GoReleaser dry run) locally to confirm archives before pushing a tag.
