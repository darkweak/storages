module github.com/darkweak/storages/badger

go 1.22.1

require (
	dario.cat/mergo v1.0.1
	github.com/darkweak/storages/core v0.0.13
	github.com/dgraph-io/badger/v3 v3.2103.5
	github.com/pierrec/lz4/v4 v4.1.22
	go.uber.org/zap v1.27.0
)

require (
	github.com/cespare/xxhash v1.1.0 // indirect
	github.com/cespare/xxhash/v2 v2.1.1 // indirect
	github.com/dgraph-io/ristretto v0.1.1 // indirect
	github.com/dustin/go-humanize v1.0.0 // indirect
	github.com/gogo/protobuf v1.3.2 // indirect
	github.com/golang/glog v0.0.0-20160126235308-23def4e6c14b // indirect
	github.com/golang/groupcache v0.0.0-20190702054246-869f871628b6 // indirect
	github.com/golang/protobuf v1.5.0 // indirect
	github.com/golang/snappy v0.0.3 // indirect
	github.com/google/flatbuffers v1.12.1 // indirect
	github.com/klauspost/compress v1.12.3 // indirect
	github.com/pkg/errors v0.9.1 // indirect
	go.opencensus.io v0.22.5 // indirect
	go.uber.org/multierr v1.10.0 // indirect
	golang.org/x/net v0.0.0-20201021035429-f5854403a974 // indirect
	golang.org/x/sys v0.0.0-20221010170243-090e33056c14 // indirect
	google.golang.org/protobuf v1.36.5 // indirect
)

replace github.com/darkweak/storages/core => ../core
