package caddy

import (
	"net/http"
	"time"

	caddy "github.com/caddyserver/caddy/v2"
	"github.com/caddyserver/caddy/v2/caddyconfig/caddyfile"
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

func parseGoRedisConfiguration(c map[string]interface{}) map[string]interface{} {
	for k := range c {
		switch k {
		case "Addrs":
			if c[k] != nil {
				if val, ok := c[k].(string); ok {
					c[k] = []string{val}
				}
			}
		}
	}

	return c
}

func parseCaddyfileRecursively(h *caddyfile.Dispenser) interface{} {
	input := make(map[string]interface{})

	for nesting := h.Nesting(); h.NextBlock(nesting); {
		val := h.Val()
		if val == "}" || val == "{" {
			continue
		}

		args := h.RemainingArgs()
		if len(args) == 1 {
			input[val] = args[0]
		} else if len(args) > 1 {
			input[val] = args
		} else {
			input[val] = parseCaddyfileRecursively(h)
		}
	}

	return input
}

func parseConfiguration(h *caddyfile.Dispenser) core.Configuration {
	var c core.Configuration

	for h.Next() {
		for nesting := h.Nesting(); h.NextBlock(nesting); {
			rootOption := h.Val()
			switch rootOption {
			case "redis":
				c.Provider = core.CacheProvider{}

				for nesting := h.Nesting(); h.NextBlock(nesting); {
					directive := h.Val()
					switch directive {
					case "path":
						urlArgs := h.RemainingArgs()
						c.Provider.Path = urlArgs[0]
					case "configuration":
						c.Provider.Configuration = parseCaddyfileRecursively(h)
						c.Provider.Configuration = parseGoRedisConfiguration(c.Provider.Configuration.(map[string]interface{}))
					}
				}
			case "stale":
				args := h.RemainingArgs()
				s, err := time.ParseDuration(args[0])

				if err == nil {
					c.Stale = s
				}
			}
		}
	}

	return c
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
