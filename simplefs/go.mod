module github.com/darkweak/storages/simplefs

go 1.22.1

replace github.com/darkweak/storages/core => ../core

require (
	github.com/darkweak/storages/core v0.0.10
	github.com/jellydator/ttlcache/v3 v3.3.0
	github.com/pierrec/lz4/v4 v4.1.21
	go.uber.org/zap v1.27.0
)

require (
	go.uber.org/multierr v1.11.0 // indirect
	golang.org/x/sync v0.8.0 // indirect
	google.golang.org/protobuf v1.34.2 // indirect
)
