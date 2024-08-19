package caddy

import (
	"net/http"

	caddy "github.com/caddyserver/caddy/v2"
	"github.com/caddyserver/caddy/v2/modules/caddyhttp"
	"github.com/darkweak/storages/core"
	"github.com/darkweak/storages/olric"
)

const moduleName = "olric"

// Olric storage.
type Olric struct {
	// Keep the handler configuration.
	core.Configuration
}

//nolint:gochecknoinits
func init() {
	caddy.RegisterModule(Olric{})
}

// CaddyModule returns the Caddy module information.
func (Olric) CaddyModule() caddy.ModuleInfo {
	return caddy.ModuleInfo{
		ID:  "storages.cache.olric",
		New: func() caddy.Module { return new(Olric) },
	}
}

// Provision to do the provisioning part.
func (b *Olric) Provision(ctx caddy.Context) error {
	logger := ctx.Logger(b)
	storer, err := olric.Factory(b.Configuration.Provider, logger.Sugar(), b.Configuration.Stale)

	if err != nil {
		return err
	}

	core.RegisterStorage(storer)

	return nil
}

func (b *Olric) ServeHTTP(rw http.ResponseWriter, rq *http.Request, next caddyhttp.Handler) error {
	return next.ServeHTTP(rw, rq)
}

// Interface guards.
var (
	_ caddy.Provisioner           = (*Olric)(nil)
	_ caddyhttp.MiddlewareHandler = (*Olric)(nil)
)
