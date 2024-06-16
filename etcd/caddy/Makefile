.PHONY:

build:
	go mod tidy
	go mod download
	XCADDY_RACE_DETECTOR=1 XCADDY_DEBUG=1 xcaddy build --with github.com/darkweak/storages/core=../../core/ --with github.com/darkweak/storages/etcd=../ --with github.com/darkweak/storages/etcd/caddy=./
	./caddy run