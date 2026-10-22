# Homepage Stats Metrics Server

A lightweight Linux metrics server for Homepage's Glances widget. It serves the
metrics Homepage needs from a static Go binary in a non-root `scratch` image. It
is a focused API implementation, not a full Glances server.

One homelab deployment observed about 10 MB of container memory, compared with
about 100 MB for a minimal Glances setup. This is an unbenchmarked observation;
actual usage varies by host and configuration.

## Quick start

The included Compose file publishes the service on localhost by default:

```sh
export STATS_IMAGE=ghcr.io/pduchnovsky/homepage-stats:latest
docker compose up -d
```

Add a Glances widget to Homepage, using an address reachable from its container
or host:

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

If Homepage runs in a separate container or on another host, `127.0.0.1` will
not work. Set `STATS_BIND_IP` to an interface reachable by Homepage. For remote
access, use the server's LAN address and firewall the port so only the Homepage
host can reach it. The service has no built-in authentication or TLS; do not
expose it publicly.

## Configuration

| Setting           | Purpose                                | Default     |
| ----------------- | -------------------------------------- | ----------- |
| `STATS_IMAGE`     | Image used by Compose                  | Required    |
| `STATS_BIND_IP`   | Host interface for the published port  | `127.0.0.1` |
| `STATS_PORT`      | HTTP listener and published port       | `61208`     |
| `PUID`            | Container user ID selected by Compose  | `65532`     |
| `GUID`            | Container group ID selected by Compose | `65532`     |
| `STATS_DISK_PATH` | Filesystem path measured by the API    | `/`         |

For a custom user, group, or port:

```sh
PUID=1000 GUID=1000 STATS_PORT=8080 docker compose up -d
```

`PUID` and `GUID` are Compose substitutions for `user:`, not variables read by
the binary. The selected user and group need read and directory traversal access
to mounted filesystems and sensors. The image does not change ownership or
permissions. `STATS_PORT` accepts 1-65535; an unprivileged port (1024 or higher)
is more portable. The healthcheck uses the same port.

By default, disk metrics describe the container's `/`, which may be its overlay
filesystem. To report a host filesystem, add a narrow read-only mount and set
`STATS_DISK_PATH` to the container path:

```yaml
environment:
  - STATS_DISK_PATH=/hostfs
volumes:
  - /path/on/host:/hostfs:ro
```

The API presents the selected filesystem as mountpoint `/`, matching Homepage's
`disk: /` selector.

## API

Endpoints accept GET and HEAD at `/api/4/<endpoint>`, `/api/3/<endpoint>`, and
`/<endpoint>`. The v3 prefix aliases this API subset; it does not provide full
Glances v3 compatibility. CPU is sampled once per second; other metrics are read
when requested.

| Endpoint     | Response                                                          |
| ------------ | ----------------------------------------------------------------- |
| `cpu`        | `total` percentage                                                |
| `load`       | `cpucore`, `min1`, `min5`, `min15` raw Linux load averages        |
| `mem`        | `total`, `used`, `free`, `available` bytes and `percent`          |
| `fs`, `disk` | One filesystem entry with `mnt_point: "/"`, bytes, and percentage |
| `sensors`    | `label`, `type`, `value`, `unit`, and optional thresholds         |
| `uptime`     | Glances-style string, for example `"2 days, 1:02:03"`             |
| `system`     | Runtime `os_name` and container/UTS `hostname`                    |
| `quicklook`  | Minimal `cpu`, `mem`, `load` summary                              |
| `health`     | HTTP 204 while responding; liveness, not metric readiness         |

Unknown routes return 404, unsupported methods 405, and failed metric collection 503. CPU returns 503 during the initial sampling second; empty sensor results are
`[]`. Common Intel, AMD, and Raspberry Pi sensor keys are recognized; unknown
keys are reported as `temperature_other`.

## Host metrics and security

CPU, load, and memory use Linux host-visible kernel counters, not per-container
cgroup utilization. Runtimes that virtualize `/proc` may report different values.
Temperature readings depend on host sensor data and supported kernel drivers.

Compose runs with a read-only root filesystem, no Linux capabilities, and no
Docker socket. Privileged mode and host PID sharing are not required.

## Images and updates

Images are published to GHCR for Linux AMD64 and ARM64 when a GitHub Release is
published. A release tag such as `v0.1.0` publishes `:0.1.0`, `:0.1`, and a
commit-specific `:sha-...` tag. Stable releases also update `:latest`; prereleases
do not. Use a release tag or digest for stable deployments.

Pull and recreate the service to apply an image update:

```sh
docker compose pull
docker compose up -d
```

Dependabot manages Go modules, Dockerfile images, and GitHub Actions. Go minor
and patch updates are grouped; major updates are separate PRs for review. The Go
version in `go.mod` selects the CI toolchain and Docker compiler. Actions are
pinned to commit SHAs. Go major updates that change import paths need a manual
source migration; Dependabot does not rewrite imports.

## Development

Use the Go version declared in `go.mod`. Container builds require Docker with
BuildKit.

```sh
go mod download all
gofmt -w main.go main_test.go
go vet ./...
go test -race ./...
CGO_ENABLED=0 go build -trimpath -o stats .
docker build --build-arg GO_VERSION="$(awk '$1 == "go" {print $2}' go.mod)" -t homepage-stats .
```

CI verifies modules, runs vet and race-enabled tests, and builds and smoke-tests
the container as a non-root user before publishing. Host-metric tests run on
Linux; schema, routing, sensor mapping, and healthcheck tests need no live
temperature sensors.

## License

MIT. See [LICENSE](LICENSE). Dependencies retain their own licenses; the image
includes notices under `/usr/share/licenses`. See
[Third-Party Notices](THIRD_PARTY_NOTICES.md) for attribution and redistribution
information.

This project is not affiliated with or endorsed by Glances or Homepage. No source
from either project is bundled or linked.
