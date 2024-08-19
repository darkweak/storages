//go:build !wasm && !wasi

package caddy

import (
	"net/http"

	caddy "github.com/caddyserver/caddy/v2"
	"github.com/caddyserver/caddy/v2/modules/caddyhttp"
	"github.com/darkweak/storages/badger"
	"github.com/darkweak/storages/core"
)

const moduleName = "badger"

// Badger storage.
type Badger struct {
	// Keep the handler configuration.
	core.Configuration
}

//nolint:gochecknoinits
func init() {
	caddy.RegisterModule(Badger{})
}

// CaddyModule returns the Caddy module information.
func (Badger) CaddyModule() caddy.ModuleInfo {
	return caddy.ModuleInfo{
		ID:  "storages.cache.badger",
		New: func() caddy.Module { return new(Badger) },
	}
}

// Provision to do the provisioning part.
func (b *Badger) Provision(ctx caddy.Context) error {
	logger := ctx.Logger(b)
	storer, err := badger.Factory(b.Configuration.Provider, logger.Sugar(), b.Configuration.Stale)

	if err != nil {
		return err
	}

	core.RegisterStorage(storer)

	return nil
}

func (b *Badger) ServeHTTP(rw http.ResponseWriter, rq *http.Request, next caddyhttp.Handler) error {
	return next.ServeHTTP(rw, rq)
}

// Interface guards.
var (
	_ caddy.Provisioner           = (*Badger)(nil)
	_ caddyhttp.MiddlewareHandler = (*Badger)(nil)
)
