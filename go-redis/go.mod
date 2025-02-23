module github.com/darkweak/storages/go-redis

go 1.22.1

replace github.com/darkweak/storages/core => ../core

require (
	github.com/darkweak/storages/core v0.0.12
	github.com/pierrec/lz4/v4 v4.1.22
	github.com/redis/go-redis/v9 v9.7.1
	go.uber.org/zap v1.27.0
)

require (
	github.com/cespare/xxhash/v2 v2.2.0 // indirect
	github.com/dgryski/go-rendezvous v0.0.0-20200823014737-9f7001d12a5f // indirect
	go.uber.org/multierr v1.10.0 // indirect
	google.golang.org/protobuf v1.36.5 // indirect
)
