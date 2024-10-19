.PHONY: bump-version dependencies generate-release golangci-lint unit-tests

MODULES_LIST=badger core etcd go-redis nats nuts olric otter redis simplefs
STORAGES_LIST=badger etcd go-redis nats nuts olric otter redis simplefs
TESTS_LIST=badger core etcd go-redis nats nuts otter redis simplefs

bump-version:
	test $(from)
	test $(to)

	# There is a bug in sed and we cannot use the storage variable in the replacement
	sed -i '' 's/github.com\/darkweak\/storages\/badger $(from)/github.com\/darkweak\/storages\/badger $(to)/' badger/caddy/go.mod
	sed -i '' 's/github.com\/darkweak\/storages\/etcd $(from)/github.com\/darkweak\/storages\/etcd $(to)/' etcd/caddy/go.mod
	sed -i '' 's/github.com\/darkweak\/storages\/go-redis $(from)/github.com\/darkweak\/storages\/go-redis $(to)/' go-redis/caddy/go.mod
	sed -i '' 's/github.com\/darkweak\/storages\/nats $(from)/github.com\/darkweak\/storages\/nats $(to)/' nats/caddy/go.mod
	sed -i '' 's/github.com\/darkweak\/storages\/nuts $(from)/github.com\/darkweak\/storages\/nuts $(to)/' nuts/caddy/go.mod
	sed -i '' 's/github.com\/darkweak\/storages\/olric $(from)/github.com\/darkweak\/storages\/olric $(to)/' olric/caddy/go.mod
	sed -i '' 's/github.com\/darkweak\/storages\/otter $(from)/github.com\/darkweak\/storages\/otter $(to)/' otter/caddy/go.mod
	sed -i '' 's/github.com\/darkweak\/storages\/redis $(from)/github.com\/darkweak\/storages\/redis $(to)/' redis/caddy/go.mod

	for storage in $(STORAGES_LIST) ; do \
		sed -i '' 's/github.com\/darkweak\/storages\/core $(from)/github.com\/darkweak\/storages\/core $(to)/' $$storage/go.mod ; \
		sed -i '' 's/github.com\/darkweak\/storages\/core $(from)/github.com\/darkweak\/storages\/core $(to)/' $$storage/caddy/go.mod ; \
	done

dependencies:
	cd core && go mod tidy ; cd - ; \
	for storage in $(STORAGES_LIST) ; do \
		cd $$storage && go mod tidy ; cd - ; \
		cd $$storage/caddy && go mod tidy ; cd - ; \
	done

generate-release:
	cd .github/workflows && ./generate_release.sh

golangci-lint:
	for storage in $(MODULES_LIST) ; do \
		cd $$storage && golangci-lint run --fix ; cd - ; \
	done

unit-tests:
	go test -v -race ./core
	for item in $(TESTS_LIST) ; do \
		go test -v -race ./$$item ; \
	done

generate-protobuf:
	buf generate
