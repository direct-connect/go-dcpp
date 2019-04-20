# Go Hub

[![Github Release](https://img.shields.io/github/release/direct-connect/go-dcpp.svg)](https://github.com/direct-connect/go-dcpp/releases)

Direct Connect hub implementation for ADC and NMDC protocols.

Requires Go 1.11+.

**Features:**

- Fully multi-threaded.
- Support NMDC, ADC and IRC users on the same hub.
- Uses a single port for all protocols (protocol auto-detection).
- Search between NMDC and ADC.
- Supports TLS for ADC (`adcs://`) and NMDC (`nmdcs://`).
- Automatic TLS certificate generation.
- HTTP(S) pinger support.
- User registration.
- User profiles and operators.
- User commands.
- Chat rooms.
- Go plugins.

**TODO:**

- Scripts.
- Spam filters.
- Get certificates from LetsEncrypt.

### Building on Linux

Download and install Go 1.12+ from [this page](https://golang.org/dl/),
or install it with Snap:

```bash
# install the Snap package manager:
sudo apt install snapd
# install the latest stable Go version:
sudo snap install --classic go
```

And build the hub binary:

```bash
go build ./cmd/go-hub
```

### Building on Windows

Download and install Go 1.12+ from [this page](https://golang.org/dl/).
You may also need to install [Git](https://git-scm.com/download/win).

To build the hub binary, run:

```bash
go build .\cmd\go-hub
```

### Running the hub

First, run the hub configuration:

```
./go-hub init
```

This will create a file called `hub.yml` with the default configuration.

To create a user with admin permissions:
```
./go-hub user add Bob password root 
```

To run the hub:

```
./go-hub serve
```

Check help for additional commands and flags:

```
./go-hub -h
```

### Profiling

To enable performance profiling:

```
./go-hub serve --pprof
```

A profiling endpoint will be available at http://localhost:6060/debug/pprof.

See [pprof](https://golang.org/pkg/net/http/pprof/) documentation for more details.

### License

BSD 3-Clause License