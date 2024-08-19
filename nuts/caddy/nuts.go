package caddy

import (
	"net/http"

	caddy "github.com/caddyserver/caddy/v2"
	"github.com/caddyserver/caddy/v2/modules/caddyhttp"
	"github.com/darkweak/storages/core"
	"github.com/darkweak/storages/nuts"
)

const moduleName = "nuts"

// Nuts storage.
type Nuts struct {
	// Keep the handler configuration.
	core.Configuration
}

//nolint:gochecknoinits
func init() {
	caddy.RegisterModule(Nuts{})
}

// CaddyModule returns the Caddy module information.
func (Nuts) CaddyModule() caddy.ModuleInfo {
	return caddy.ModuleInfo{
		ID:  "storages.cache.nuts",
		New: func() caddy.Module { return new(Nuts) },
	}
}

// Provision to do the provisioning part.
func (b *Nuts) Provision(ctx caddy.Context) error {
	logger := ctx.Logger(b)
	storer, err := nuts.Factory(b.Configuration.Provider, logger.Sugar(), b.Configuration.Stale)

	if err != nil {
		return err
	}

	core.RegisterStorage(storer)

	return nil
}

func (b *Nuts) ServeHTTP(rw http.ResponseWriter, rq *http.Request, next caddyhttp.Handler) error {
	return next.ServeHTTP(rw, rq)
}

// Interface guards.
var (
	_ caddy.Provisioner           = (*Nuts)(nil)
	_ caddyhttp.MiddlewareHandler = (*Nuts)(nil)
)
