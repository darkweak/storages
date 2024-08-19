//go:build !wasm && !wasi

package caddy

import (
	"net/http"

	caddy "github.com/caddyserver/caddy/v2"
	"github.com/caddyserver/caddy/v2/modules/caddyhttp"
	"github.com/darkweak/storages/core"
	"github.com/darkweak/storages/nats"
)

const moduleName = "nats"

// Nats storage.
type Nats struct {
	// Keep the handler configuration.
	core.Configuration
}

//nolint:gochecknoinits
func init() {
	caddy.RegisterModule(Nats{})
}

// CaddyModule returns the Caddy module information.
func (Nats) CaddyModule() caddy.ModuleInfo {
	return caddy.ModuleInfo{
		ID:  "storages.cache.nats",
		New: func() caddy.Module { return new(Nats) },
	}
}

// Provision to do the provisioning part.
func (b *Nats) Provision(ctx caddy.Context) error {
	logger := ctx.Logger(b)
	storer, err := nats.Factory(b.Configuration.Provider, logger.Sugar(), b.Configuration.Stale)

	if err != nil {
		return err
	}

	core.RegisterStorage(storer)

	return nil
}

func (b *Nats) ServeHTTP(rw http.ResponseWriter, rq *http.Request, next caddyhttp.Handler) error {
	return next.ServeHTTP(rw, rq)
}

// Interface guards.
var (
	_ caddy.Provisioner           = (*Nats)(nil)
	_ caddyhttp.MiddlewareHandler = (*Nats)(nil)
)
