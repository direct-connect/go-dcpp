# DCPing

A command line DC pinger.

## Build

On Linux:
```bash
go build ./cmd/dcping
```

On Windows:
```bash
go build .\cmd\dcping
```

## Commands

### Protocol probe

This command detects the protocol of a DC hub and prints a canonical address.

```
$ dcping probe example.org
dchub://example.org:411

$ dcping probe example2.org:8000
adcs://example2.org:8000
```

### Ping the hub

This command "pings" the hub and returns its information.

```
$ dcping ping example.org
{"name":"Example Hub","desc":"Hub description","addr":["adcs://example.org:411"],"encoding":"utf-8","soft":{"name":"test-hub","vers":"1.0","ext":["BASE","TIGR","PING"]},"users":123,"files":2356,"share":115350897664}
```

It also supports XML output:

```
$ dcping ping --out=xml example.org
<Hub Name="Example Hub" Description="Hub description" Address="adcs://example.org:411" Encoding="UTF-8" Users="123" Shared="115350897664" Software="test-hub" Status="Online"></Hub>
```