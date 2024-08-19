package caddy

import (
	"net/http"

	caddy "github.com/caddyserver/caddy/v2"
	"github.com/caddyserver/caddy/v2/modules/caddyhttp"
	"github.com/darkweak/storages/core"
	"github.com/darkweak/storages/otter"
)

const moduleName = "otter"

// Otter storage.
type Otter struct {
	// Keep the handler configuration.
	core.Configuration
}

//nolint:gochecknoinits
func init() {
	caddy.RegisterModule(Otter{})
}

// CaddyModule returns the Caddy module information.
func (Otter) CaddyModule() caddy.ModuleInfo {
	return caddy.ModuleInfo{
		ID:  "storages.cache.otter",
		New: func() caddy.Module { return new(Otter) },
	}
}

// Provision to do the provisioning part.
func (b *Otter) Provision(ctx caddy.Context) error {
	logger := ctx.Logger(b)
	storer, err := otter.Factory(b.Configuration.Provider, logger.Sugar(), b.Configuration.Stale)

	if err != nil {
		return err
	}

	core.RegisterStorage(storer)

	return nil
}

func (b *Otter) ServeHTTP(rw http.ResponseWriter, rq *http.Request, next caddyhttp.Handler) error {
	return next.ServeHTTP(rw, rq)
}

// Interface guards.
var (
	_ caddy.Provisioner           = (*Otter)(nil)
	_ caddyhttp.MiddlewareHandler = (*Otter)(nil)
)
