package caddy

import (
	"net/http"
	"strconv"
	"time"

	caddy "github.com/caddyserver/caddy/v2"
	"github.com/caddyserver/caddy/v2/caddyconfig/caddyfile"
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

func parseBadgerConfiguration(c map[string]interface{}) map[string]interface{} {
	for k, v := range c {
		switch k {
		case "Dir", "ValueDir":
			c[k] = v
		case "SyncWrites", "ReadOnly", "InMemory", "MetricsEnabled", "CompactL0OnClose", "LmaxCompaction", "VerifyValueChecksum", "BypassLockGuard", "DetectConflicts":
			c[k] = true
		case "NumVersionsToKeep", "NumGoroutines", "MemTableSize", "BaseTableSize", "BaseLevelSize", "LevelSizeMultiplier", "TableSizeMultiplier", "MaxLevels", "ValueThreshold", "NumMemtables", "BlockSize", "BlockCacheSize", "IndexCacheSize", "NumLevelZeroTables", "NumLevelZeroTablesStall", "ValueLogFileSize", "NumCompactors", "ZSTDCompressionLevel", "ChecksumVerificationMode", "NamespaceOffset":
			c[k], _ = strconv.Atoi(v.(string))
		case "Compression", "ValueLogMaxEntries":
			c[k], _ = strconv.ParseUint(v.(string), 10, 32)
		case "VLogPercentile", "BloomFalsePositive":
			c[k], _ = strconv.ParseFloat(v.(string), 64)
		case "EncryptionKey":
			c[k] = []byte(v.(string))
		case "EncryptionKeyRotationDuration":
			c[k], _ = time.ParseDuration(v.(string))
		}
	}

	return c
}

func parseConfiguration(h *caddyfile.Dispenser) (c core.Configuration) {
	for h.Next() {
		for nesting := h.Nesting(); h.NextBlock(nesting); {
			rootOption := h.Val()
			switch rootOption {
			case "badger":
				c.Provider = core.CacheProvider{}
				for nesting := h.Nesting(); h.NextBlock(nesting); {
					directive := h.Val()
					switch directive {
					case "path":
						urlArgs := h.RemainingArgs()
						c.Provider.Path = urlArgs[0]
					case "configuration":
						c.Provider.Configuration = parseCaddyfileRecursively(h)
						c.Provider.Configuration = parseBadgerConfiguration(c.Provider.Configuration.(map[string]interface{}))
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
	storer, err := badger.Factory(b.Configuration.Provider, logger, b.Configuration.Stale)
	if err != nil {
		return err
	}

	core.RegisterStorage(storer)

	return nil
}

func (b *Badger) ServeHTTP(rw http.ResponseWriter, rq *http.Request, next caddyhttp.Handler) error {
	return next.ServeHTTP(rw, rq)
}

// Interface guards
var (
	_ caddy.Provisioner           = (*Badger)(nil)
	_ caddyhttp.MiddlewareHandler = (*Badger)(nil)
)
