package main

import (
	"log"
	"net/http"

	"github.com/ante-dk/envconfig"
)

type Config struct {
	ExpectedItems  int   `envconfig:"EXPECTED_ITEMS" required:"true"`
	CacheMetrics   bool  `envconfig:"RECORD_METRICS" default:"false"`
	MaxCacheSizeMB int64 `envconfig:"MAX_CACHE_SIZE_MB" default:"100"`

	ProxyTargetURL string `envconfig:"PROXY_TARGET_URL" required:"true"`
}

func main() {
	cfg := Config{}
	err := envconfig.Process("", &cfg)
	if err != nil {
		log.Fatal(err)
	}

	size := cfg.MaxCacheSizeMB * 1000 * 1000
	proxy, err := NewHttpCacheProxy(
		cfg.ProxyTargetURL,
		cfg.ExpectedItems,
		cfg.CacheMetrics,
		size,
	)
	if err != nil {
		log.Fatal(err)
	}

	http.HandleFunc("/", proxy.Handle)
	log.Fatal(http.ListenAndServe(":4242", nil))
}
