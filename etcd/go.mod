module github.com/darkweak/storages/etcd

go 1.22.1

replace github.com/darkweak/storages/core => ../core

require (
	github.com/darkweak/storages/core v0.0.7
	github.com/pierrec/lz4/v4 v4.1.21
	go.etcd.io/etcd/client/v3 v3.5.14
	go.uber.org/zap v1.27.0
	google.golang.org/grpc v1.64.0
)

require (
	github.com/coreos/go-semver v0.3.0 // indirect
	github.com/coreos/go-systemd/v22 v22.3.2 // indirect
	github.com/gogo/protobuf v1.3.2 // indirect
	github.com/golang/protobuf v1.5.4 // indirect
	go.etcd.io/etcd/api/v3 v3.5.14 // indirect
	go.etcd.io/etcd/client/pkg/v3 v3.5.14 // indirect
	go.uber.org/multierr v1.11.0 // indirect
	golang.org/x/net v0.25.0 // indirect
	golang.org/x/sys v0.20.0 // indirect
	golang.org/x/text v0.15.0 // indirect
	google.golang.org/genproto/googleapis/api v0.0.0-20240506185236-b8a5c65736ae // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20240429193739-8cf5692501f6 // indirect
	google.golang.org/protobuf v1.34.2 // indirect
)
