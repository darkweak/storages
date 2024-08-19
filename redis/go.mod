module github.com/darkweak/storages/redis

go 1.22.1

replace github.com/darkweak/storages/core => ../core

require (
	github.com/darkweak/storages/core v0.0.8
	github.com/pierrec/lz4/v4 v4.1.21
	github.com/redis/rueidis v1.0.39
	go.uber.org/zap v1.27.0
)

require (
	go.uber.org/multierr v1.11.0 // indirect
	golang.org/x/net v0.25.0 // indirect
	golang.org/x/sys v0.20.0 // indirect
	google.golang.org/protobuf v1.34.2 // indirect
)
