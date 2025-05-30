module github.com/darkweak/storages/nuts

go 1.22.1

replace github.com/darkweak/storages/core => ../core

require (
	dario.cat/mergo v1.0.1
	github.com/darkweak/storages/core v0.0.15
	github.com/nutsdb/nutsdb v1.0.4
	github.com/pierrec/lz4/v4 v4.1.22
	go.uber.org/zap v1.27.0
)

require (
	github.com/antlabs/stl v0.0.1 // indirect
	github.com/antlabs/timer v0.0.11 // indirect
	github.com/bwmarrin/snowflake v0.3.0 // indirect
	github.com/gofrs/flock v0.8.1 // indirect
	github.com/pkg/errors v0.9.1 // indirect
	github.com/tidwall/btree v1.6.0 // indirect
	github.com/xujiajun/mmap-go v1.0.1 // indirect
	github.com/xujiajun/utils v0.0.0-20220904132955-5f7c5b914235 // indirect
	go.uber.org/multierr v1.10.0 // indirect
	golang.org/x/sys v0.15.0 // indirect
	google.golang.org/protobuf v1.36.5 // indirect
)
