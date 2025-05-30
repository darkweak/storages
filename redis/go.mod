module github.com/darkweak/storages/redis

go 1.22.1

replace github.com/darkweak/storages/core => ../core

require (
	github.com/darkweak/storages/core v0.0.15
	github.com/pierrec/lz4/v4 v4.1.22
	github.com/redis/rueidis v1.0.54
	go.uber.org/zap v1.27.0
)

require (
	go.uber.org/multierr v1.10.0 // indirect
	golang.org/x/sys v0.29.0 // indirect
	google.golang.org/protobuf v1.36.5 // indirect
)
