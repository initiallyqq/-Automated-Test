package main

import (
	"context"
	"fmt"
	"os"

	"automated-test/internal/api"
	"automated-test/internal/config"
)

func main() {
	if err := config.LoadEnvLocal(); err != nil {
		fmt.Fprintln(os.Stderr, "warning: failed to load .env.local:", err)
	}
	if err := api.NewServer(config.Default()).ListenAndServe(context.Background(), "127.0.0.1:8080"); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}
