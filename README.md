# tibcoin

Simple implementation of a cryptocurrency over Blockchain in Go.

## Getting started

In order to run Peerster:

```
go run *.go -name Node -GUIPort 8080 -UIPort 8090 -gossipAddr <IP>:<Port> -rtimer 10 -peers <IP>:<Port>,<IP>:<Port>[...]
```

Optional flags:

* `-tibcoin`: start the tibcoin node
* `-miner`: enable mining the tibcoin node

The UI is then available on `localhost:8080`.