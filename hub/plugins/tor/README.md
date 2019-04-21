# Tor server plugin

This plugin allows to publish the hub as an onion service. Note that clients will need to
run Tor as well to use it properly.

Client-client connection will only work if the client is aware of the fact that it uses
Tor network.

## Building

On Linux:
```
cd ./hub/plugins/tor
go build -buildmode=plugin -o tor.so -v
```

On Windows:
```
chdir hub\plugins\tor
go build -buildmode=plugin -o tor.dll -v
```

Build may take a significant amount of time since it will compile Tor from source.

## Running

Copy the `tor.so`/`tor.dll` to a `./plugins/` directory relative to the `go-hub`.

Hub will automatically load the plugin on start:
```
loading plugins in: plugins
...

Tor version: tor 0.3.5.8-dev
...
Tor address: xxxxxxxxxxxxxxxx.onion:1411
```

You can now use `adcs://xxxxxxxxxxxxxxxx.onion:1411` address to connect to the hub.

It is necessary to run Tor locally on the client's machine and configure it to use Tor's SOCKS proxy.