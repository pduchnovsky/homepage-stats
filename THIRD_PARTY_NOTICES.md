# Third-Party Notices

The application uses [gopsutil v3](https://github.com/shirou/gopsutil),
Copyright (c) 2014 WAKAYAMA Shirou, under BSD-3-Clause terms. Its license also
includes notices for material from Go's encoding/binary package.

Go and transitive modules retain their own licenses. The image build copies
the Go license and module-root LICENSE, COPYING, NOTICE, and PATENTS files
into `/usr/share/licenses`. That collection includes modules in the dependency
graph, some of which may not be linked on a particular target platform.
Redistributions must preserve the notices required by the included licenses.

To generate the same notices for a standalone binary distribution:

```sh
go mod download all
sh scripts/licenses.sh licenses
```

Standalone binary distributions include the generated `licenses` directory,
this file, and the project LICENSE. The image build fails if a dependency license
notice cannot be found.

This project is an independent implementation of a small Glances-compatible
HTTP API. It does not bundle or link Glances or Homepage, and is not affiliated
with or endorsed by either project. No Glances or Homepage source is included.
