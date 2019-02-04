# go-dcpp

Go library and hub implementation for ADC and NMDC protocols.

Requires Go 1.11+.

## Hub

### Building on Linux

Download and install Go 1.11+ from [this page](https://golang.org/dl/),
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

Download and install Go 1.11+ from [this page](https://golang.org/dl/).
You may also need to install [Git](https://git-scm.com/download/win).

To build the hub binary, run:

```bash
go build cmd\go-hub
```

### Running the hub

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

## License

BSD 3-Clause License