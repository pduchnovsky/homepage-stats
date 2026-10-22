# AGENTS.md

## Project

`homepage-stats` is a small Go metrics server implementing the Glances API subset
used by Homepage's information widget.

## Rules

- Preserve the documented endpoints, JSON fields, status codes, and Homepage compatibility.
- Keep the server lightweight and the container unprivileged; do not add host PID sharing,
  broad host mounts, Linux capabilities, or Docker socket access without an explicit requirement.
- Use placeholder hostnames, addresses, and paths in examples; do not add personal deployment details.
- Keep the Go toolchain version in `go.mod` as the source of truth for CI and Docker builds.
- Keep changes focused, explain non-obvious behavior, and update documentation when behavior or configuration changes.
- Do not commit changes or create branches unless explicitly requested.

## Coding and testing

- Follow standard Go conventions and run `gofmt` on changed Go files.
- Add or update tests in `main_test.go` for API, schema, routing, and metric behavior changes.
- Run `go vet -mod=readonly ./...` and `go test -mod=readonly -race -cover ./...` for Go changes.
- For container changes, build and smoke-test the image using the repository's CI workflow as a guide.
- Preserve the existing static `scratch` image and minimal runtime dependencies unless requirements change.

## Dependency updates

- Dependabot manages Go modules, Dockerfile images, and GitHub Actions.
- Review major Go dependency updates carefully. Dependabot does not rewrite source import paths when a module changes its path; perform and test that migration manually.