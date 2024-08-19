module github.com/darkweak/storages/nats

go 1.22.1

replace github.com/darkweak/storages/core => ../core

require (
	dario.cat/mergo v1.0.0
	github.com/darkweak/storages/core v0.0.8
	github.com/nats-io/nats.go v1.36.0
	github.com/pierrec/lz4/v4 v4.1.21
	go.uber.org/zap v1.27.0
)

require (
	github.com/klauspost/compress v1.17.8 // indirect
	github.com/nats-io/nkeys v0.4.7 // indirect
	github.com/nats-io/nuid v1.0.1 // indirect
	go.uber.org/multierr v1.11.0 // indirect
	golang.org/x/crypto v0.23.0 // indirect
	golang.org/x/sys v0.20.0 // indirect
	google.golang.org/protobuf v1.34.2 // indirect
)
