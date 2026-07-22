package main

import (
	"log"
	"os"

	"github.com/bakhod1r/oneenv"
)

// Config is the shop's entire runtime configuration, read from the environment
// and a .env file with oneenv.
//
// It replaces the scattered os.Getenv calls the example used to make: one
// struct with defaults and descriptions is both the single source of truth and,
// via oneenv's Usage/Example helpers, its own documentation.
type Config struct {
	Port      int    `env:"PORT" default:"8080" desc:"HTTP port for the REST API and console"`
	GRPCAddr  string `env:"GRPC_ADDR" default:":50051" desc:"address the gRPC server listens on"`
	DB        string `env:"SHOP_DB" default:"shop.db" desc:"SQLite database path (:memory: for ephemeral)"`
	BasePath  string `env:"SPECTER_BASE_PATH" desc:"where the console mounts; empty means /docs"`
	AccessKey string `env:"SPECTER_KEY,secret" desc:"gate the console behind ?key=; empty leaves it open"`
	AdminURL  string `env:"ADMIN_URL" desc:"admin panel URL; adds an Admin button to the console"`
}

// loadConfig reads .env (then .env.local, which wins) and the process
// environment into a Config.
//
// Only files that exist are passed to oneenv: WithFiles treats a listed-but-
// missing file as an error, so handing it a fixed list would make .env
// mandatory. Filtering first keeps both files optional — the example runs with
// no .env at all, on the defaults declared above.
func loadConfig() Config {
	var files []string
	for _, name := range []string{".env", ".env.local"} {
		if _, err := os.Stat(name); err == nil {
			files = append(files, name)
		}
	}
	cfg, err := oneenv.Parse[Config](oneenv.WithFiles(files...))
	if err != nil {
		log.Fatalf("shop: config: %v", err)
	}
	return *cfg
}
