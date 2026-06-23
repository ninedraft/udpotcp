# udpotcp

`udpotcp` forwards one UDP endpoint through one TCP connection.

The client listens for UDP packets on localhost, sends each datagram as a framed TCP payload to the configured server, and sends replies back to the last local UDP peer. The server accepts TCP clients and forwards their frames to one configured UDP endpoint.

This is useful for UDP services that need to cross a TCP-only path. The current transport is plain TCP framing; encryption and authentication are not implemented in this repository yet.

## Build

```sh
go test ./...
go build -trimpath -o udpotcp .
```

Windows amd64 cross-build:

```sh
CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -trimpath -o udpotcp.exe .
```

## Run

Server side, next to the UDP service:

```sh
udpotcp server -listen :34197 -udp 127.0.0.1:34197
```

Client side:

```sh
udpotcp client -listen 127.0.0.1:34197 -server example.com:34197
```

For Factorio, point the game client at `127.0.0.1:34197` when using the default client listen address.

Check whether a server TCP listener is reachable:

```sh
udpotcp healthcheck -server example.com:34197
```

When `-server` is omitted, healthcheck dials `127.0.0.1:34197`. Use `-timeout` to override the default `5s` dial timeout.

## JSON Configs

`server.json`:

```json
{
  "listen": ":34197",
  "udp": "127.0.0.1:34197"
}
```

`client.json`:

```json
{
  "listen": "127.0.0.1:34197",
  "server": "example.com:34197"
}
```

Run with an explicit config path:

```sh
udpotcp server.json
udpotcp client.json
```

You can also combine subcommands with `-config`; explicit flags override values from the JSON file:

```sh
udpotcp server -config server.json -listen :34198
udpotcp client -config client.json -server example.com:34198
```

If `udpotcp` is started without arguments, it looks for `server.json` or `client.json` in the current directory and runs that mode. Keep only one of those files in the runtime directory for automatic discovery.

## Docker

Build with BuildKit:

```sh
DOCKER_BUILDKIT=1 docker build -t udpotcp .
```

Run a server container with a mounted config:

```sh
docker run --rm -v "$PWD/server.json:/config/server.json:ro" -p 34197:34197 udpotcp
```

## Logs

Logs are optimized for direct terminal reading. They show listener addresses, TCP connection state, UDP peers, packet direction, and datagram sizes.

## License

MIT. See [LICENSE](LICENSE).
