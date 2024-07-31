package caddy

import (
	"net/http"
	"time"

	caddy "github.com/caddyserver/caddy/v2"
	"github.com/caddyserver/caddy/v2/caddyconfig/caddyfile"
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

func parseConfiguration(h *caddyfile.Dispenser) (c core.Configuration) {
	for h.Next() {
		for nesting := h.Nesting(); h.NextBlock(nesting); {
			rootOption := h.Val()
			switch rootOption {
			case "otter":
				c.Provider = core.CacheProvider{}
				for nesting := h.Nesting(); h.NextBlock(nesting); {
					directive := h.Val()
					switch directive {
					case "path":
						urlArgs := h.RemainingArgs()
						c.Provider.Path = urlArgs[0]
					case "configuration":
						c.Provider.Configuration = parseCaddyfileRecursively(h)
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

	return
}

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

// Interface guards
var (
	_ caddy.Provisioner           = (*Otter)(nil)
	_ caddyhttp.MiddlewareHandler = (*Otter)(nil)
)
