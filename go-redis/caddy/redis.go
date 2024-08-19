package caddy

import (
	"errors"
	"net/http"

	caddy "github.com/caddyserver/caddy/v2"
	"github.com/caddyserver/caddy/v2/modules/caddyhttp"
	"github.com/darkweak/storages/core"
	"github.com/darkweak/storages/go-redis"
)

const moduleName = "redis"

// Redis storage.
type Redis struct {
	// Keep the handler configuration.
	core.Configuration
}

func init() {
	caddy.RegisterModule(Redis{})
}

// CaddyModule returns the Caddy module information.
func (Redis) CaddyModule() caddy.ModuleInfo {
	return caddy.ModuleInfo{
		ID:  "storages.cache.redis",
		New: func() caddy.Module { return new(Redis) },
	}
}

// Provision to do the provisioning part.
func (b *Redis) Provision(ctx caddy.Context) error {
	if b.Configuration.Provider.Configuration == nil && b.Configuration.Provider.URL == "" {
		return errors.New("No configuration given, cannot instantiate go-redis")
	}

	logger := ctx.Logger(b)
	storer, err := redis.Factory(b.Configuration.Provider, logger.Sugar(), b.Configuration.Stale)

	if err != nil {
		return err
	}

	core.RegisterStorage(storer)

	return nil
}

func (b *Redis) ServeHTTP(rw http.ResponseWriter, rq *http.Request, next caddyhttp.Handler) error {
	return next.ServeHTTP(rw, rq)
}

// Interface guards.
var (
	_ caddy.Provisioner           = (*Redis)(nil)
	_ caddyhttp.MiddlewareHandler = (*Redis)(nil)
)
