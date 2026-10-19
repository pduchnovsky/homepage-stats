# homepage-stats

A small Linux metrics server in Go for Homepage's Glances information widget.
One static binary, a non-root `scratch` image, and a limited Glances-compatible
JSON API. No Python runtime, database, Docker socket, or host PID namespace needed.

This independent implementation supports the API subset used by Homepage's
Glances information widget, not the complete Glances API.

Observed container memory usage in one deployment was approximately 10 MB,
compared with approximately 100 MB for a minimal Glances setup. These are
deployment observations, not a controlled benchmark. Usage varies with host and
configuration; CPU performance has not been benchmarked.

## Container Images

Container images are built for Linux AMD64 and ARM64. The publishing workflow
targets GHCR, with `latest` following `main`. Version tags such as `v0.1.0` produce
`:0.1.0` and `:0.1` image tags, alongside commit-specific `:sha-...` tags.
Use a release tag or digest for stable deployments.

Pull requests run checks without publishing. CI and Docker use the Go version
declared in `go.mod`.

## Run

The included Compose configuration requires an image reference. Replace `OWNER`
with the image's GHCR namespace:

```sh
export STATS_IMAGE=ghcr.io/OWNER/homepage-stats:latest
export STATS_BIND_IP=127.0.0.1
docker compose up -d
```

By default, disk metrics measure the container's `/`. To report a host filesystem
instead, add a read-only bind mount and set `STATS_DISK_PATH` to its container path.
Merge these settings into the service, retaining its existing `STATS_PORT` entry:

```yaml
environment:
  - STATS_DISK_PATH=/hostfs
volumes:
  - /path/on/host:/hostfs:ro
```

The selected filesystem is reported as API mountpoint `/`, keeping Homepage's
`disk: /` selector compatible. Only one filesystem is reported; prefer a narrow
directory mount over exposing the entire host root.

| Setting           | Purpose                                        | Default     |
| ----------------- | ---------------------------------------------- | ----------- |
| `STATS_IMAGE`     | Container image reference used by Compose      | Required    |
| `STATS_BIND_IP`   | Host interface for the published port          | `127.0.0.1` |
| `STATS_PORT`      | HTTP listener and Compose published port       | `61208`     |
| `PUID`            | Numeric container user ID selected by Compose  | `65532`     |
| `GUID`            | Numeric container group ID selected by Compose | `65532`     |
| `STATS_DISK_PATH` | Filesystem path read by the application        | `/`         |

For a custom identity and port:

```sh
PUID=1000 GUID=1000 STATS_PORT=8080 docker compose up -d
```

Settings can also be persisted in a Compose `.env` file. `PUID` and `GUID` are
Compose substitutions for `user:`, not environment variables read by the binary.
With `docker run`, use `--user 1000:1000 -e STATS_PORT=8080 -p 8080:8080`.
The selected user/group must have read and directory traversal access to the
mounted filesystem and sensors. No ownership or permission changes are performed.
`STATS_PORT` accepts integers from 1 to 65535; use an unprivileged port (1024 or
above) for portability. The built-in healthcheck uses the same port.

### Security and Host Access

For remote Homepage access, set `STATS_BIND_IP` to the server's LAN IP and restrict
the configured TCP port to the Homepage host with a firewall. There is no built-in
authentication or TLS. Do not expose the endpoint publicly. Binding a LAN IP is
not an allowlist.

Compose uses the image's built-in `/stats healthcheck`, read-only root
filesystem, no Linux capabilities, and no Docker socket. Temperature availability
depends on sensor data exposed by the container runtime and supported kernel
drivers. Privileged mode is not required.

CPU, load, and memory come from host-visible Linux kernel counters, not
per-container cgroup utilization. Runtimes that virtualize `/proc` may report
different values. Host PID sharing is not required and does not change the
container's filesystem mounts or hostname.

## Homepage

Add a Glances information widget using the metrics server's address:

```yaml
- glances:
    url: http://METRICS_SERVER:61208
    version: 4
    cpu: true
    mem: true
    cputemp: true
    disk: /
    uptime: true
    label: My Server
```

Use the configured `STATS_PORT` in the widget URL when changing the default port.

Homepage v2.6.0 formats `min15` as a percentage and labels the expanded temperature
threshold `Warn`. These display choices are controlled by Homepage; the API
returns raw load averages and sensor warning thresholds. Homepage averages
matching CPU temperature sensors. Its default prefixes select individual `Core`
sensors on Intel systems, while package sensors remain available through the API.

## API

Endpoints accept GET and HEAD at `/api/4/<endpoint>`, `/api/3/<endpoint>`, and
`/<endpoint>`. The v3 prefix is an alias for the same API subset, not full Glances
v3 compatibility.

| Endpoint     | Response                                                                |
| ------------ | ----------------------------------------------------------------------- |
| `cpu`        | `total` percentage, sampled once per second and shared between requests |
| `load`       | `cpucore`, `min1`, `min5`, `min15`, with raw Linux load averages        |
| `mem`        | `total`, `used`, `free`, `available` bytes and `percent`                |
| `fs`, `disk` | One filesystem entry, with `mnt_point: "/"`, bytes and percentage       |
| `sensors`    | Records with `label`, `type`, `value`, `unit`, and optional thresholds  |
| `uptime`     | Glances-style string, for example `"2 days, 1:02:03"`                   |
| `system`     | Runtime `os_name` and container/UTS `hostname`                          |
| `quicklook`  | Minimal `cpu`, `mem`, `load` summary, not the full Glances summary API  |
| `health`     | HTTP 204 while the server responds; liveness, not metric readiness      |

Unknown routes return 404, unsupported methods 405, and failed metric collection 503. CPU returns 503 during the initial sampling second. Empty sensors are `[]`.
The sensor adapter recognizes common Intel/AMD/Raspberry Pi CPU driver keys;
unknown driver keys are classified as `temperature_other`.

Without a host filesystem bind mount, `/` metrics describe the container's
filesystem, which may be the overlay backing filesystem rather than a host disk.

## Development

Development requires the Go version declared in `go.mod`. Container builds
require Docker with BuildKit.

```sh
go mod download all
gofmt -w main.go main_test.go
go vet ./...
go test -race ./...
CGO_ENABLED=0 go build -trimpath -o stats .
docker build --build-arg GO_VERSION="$(awk '$1 == "go" {print $2}' go.mod)" -t homepage-stats .
```

CI runs `go vet`, race-enabled tests, and a container healthcheck smoke test with
a custom non-root UID/GID and port before publishing. Host-metric tests run on
Linux; schema, routing, sensor mapping, and
healthcheck tests do not require live temperature sensors.

## Updates

Pull the selected image tag and recreate the service to apply image updates:

```sh
docker compose pull
docker compose up -d
```

Dependency maintenance is configured through Renovate for Go modules, Dockerfile
dependencies, and GitHub Actions. Automerge is disabled; major module upgrades
require approval. The Go version in `go.mod` selects both the CI toolchain and
Docker compiler. Actions are pinned to commit SHAs so moved version tags cannot
silently change workflow code; Renovate proposes updates to these pins.
The compiler image uses a version tag derived from `go.mod`, not a digest pin.

## License and Attribution

MIT-licensed; see [LICENSE](LICENSE). Dependencies retain their own licenses;
images include their notices under `/usr/share/licenses`. See
[Third-Party Notices](THIRD_PARTY_NOTICES.md) for attribution and redistribution
information.

This project is not affiliated with or endorsed by Glances or Homepage. No source
from either project is bundled or linked.
