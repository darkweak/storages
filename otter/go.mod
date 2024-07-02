module github.com/darkweak/storages/otter

go 1.22.1

replace github.com/darkweak/storages/core => ../core

require (
	github.com/darkweak/storages/core v0.0.3
	github.com/maypok86/otter v1.2.1
	github.com/pierrec/lz4/v4 v4.1.21
	go.uber.org/zap v1.27.0
)

require (
	github.com/dolthub/maphash v0.1.0 // indirect
	github.com/gammazero/deque v0.2.1 // indirect
	go.uber.org/multierr v1.11.0 // indirect
)
