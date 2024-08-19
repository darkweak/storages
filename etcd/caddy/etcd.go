//go:build !wasm && !wasi

package caddy

import (
	"net/http"

	caddy "github.com/caddyserver/caddy/v2"
	"github.com/caddyserver/caddy/v2/modules/caddyhttp"
	"github.com/darkweak/storages/core"
	"github.com/darkweak/storages/etcd"
)

const moduleName = "etcd"

// Etcd storage.
type Etcd struct {
	// Keep the handler configuration.
	core.Configuration
}

//nolint:gochecknoinits
func init() {
	caddy.RegisterModule(Etcd{})
}

// CaddyModule returns the Caddy module information.
func (Etcd) CaddyModule() caddy.ModuleInfo {
	return caddy.ModuleInfo{
		ID:  "storages.cache.etcd",
		New: func() caddy.Module { return new(Etcd) },
	}
}

// Provision to do the provisioning part.
func (b *Etcd) Provision(ctx caddy.Context) error {
	logger := ctx.Logger(b)
	storer, err := etcd.Factory(b.Configuration.Provider, logger.Sugar(), b.Configuration.Stale)

	if err != nil {
		return err
	}

	core.RegisterStorage(storer)

	return nil
}

func (b *Etcd) ServeHTTP(rw http.ResponseWriter, rq *http.Request, next caddyhttp.Handler) error {
	return next.ServeHTTP(rw, rq)
}

// Interface guards.
var (
	_ caddy.Provisioner           = (*Etcd)(nil)
	_ caddyhttp.MiddlewareHandler = (*Etcd)(nil)
)
