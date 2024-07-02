.PHONY: bump-version generate-release

STORAGES_LIST=badger etcd nuts olric otter redis

bump-version:
	test $(from)
	test $(to)

	# There is a bug in sed and we cannot use the storage variable in the replacement
	sed -i '' 's/github.com\/darkweak\/storages\/badger $(from)/github.com\/darkweak\/storages\/badger $(to)/' badger/caddy/go.mod
	sed -i '' 's/github.com\/darkweak\/storages\/etcd $(from)/github.com\/darkweak\/storages\/etcd $(to)/' etcd/caddy/go.mod
	sed -i '' 's/github.com\/darkweak\/storages\/nuts $(from)/github.com\/darkweak\/storages\/nuts $(to)/' nuts/caddy/go.mod
	sed -i '' 's/github.com\/darkweak\/storages\/olric $(from)/github.com\/darkweak\/storages\/olric $(to)/' olric/caddy/go.mod
	sed -i '' 's/github.com\/darkweak\/storages\/otter $(from)/github.com\/darkweak\/storages\/otter $(to)/' otter/caddy/go.mod
	sed -i '' 's/github.com\/darkweak\/storages\/redis $(from)/github.com\/darkweak\/storages\/redis $(to)/' redis/caddy/go.mod

	for storage in $(STORAGES_LIST) ; do \
		sed -i '' 's/github.com\/darkweak\/storages\/core $(from)/github.com\/darkweak\/storages\/core $(to)/' $$storage/go.mod ; \
		sed -i '' 's/github.com\/darkweak\/storages\/core $(from)/github.com\/darkweak\/storages\/core $(to)/' $$storage/caddy/go.mod ; \
	done

generate-release:
	cd .github/workflows && ./generate_release.sh