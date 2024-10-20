package caddy

import (
	"net/http"

	caddy "github.com/caddyserver/caddy/v2"
	"github.com/caddyserver/caddy/v2/modules/caddyhttp"
	"github.com/darkweak/storages/core"
	"github.com/darkweak/storages/simplefs"
)

const moduleName = "simplefs"

// Simplefs storage.
type Simplefs struct {
	// Keep the handler configuration.
	core.Configuration
}

//nolint:gochecknoinits
func init() {
	caddy.RegisterModule(Simplefs{})
}

// CaddyModule returns the Caddy module information.
func (Simplefs) CaddyModule() caddy.ModuleInfo {
	return caddy.ModuleInfo{
		ID:  "storages.cache.simplefs",
		New: func() caddy.Module { return new(Simplefs) },
	}
}

// Provision to do the provisioning part.
func (b *Simplefs) Provision(ctx caddy.Context) error {
	logger := ctx.Logger(b)
	storer, err := simplefs.Factory(b.Configuration.Provider, logger.Sugar(), b.Configuration.Stale)

	if err != nil {
		return err
	}

	core.RegisterStorage(storer)

	return nil
}

func (b *Simplefs) ServeHTTP(rw http.ResponseWriter, rq *http.Request, next caddyhttp.Handler) error {
	return next.ServeHTTP(rw, rq)
}

// Interface guards.
var (
	_ caddy.Provisioner           = (*Simplefs)(nil)
	_ caddyhttp.MiddlewareHandler = (*Simplefs)(nil)
)
