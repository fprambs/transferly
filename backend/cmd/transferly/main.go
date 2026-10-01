// Transferly runs the web server and its administrative subcommands.
//
// Usage:
//
//	transferly [serve]
//	transferly migrate [up|status]
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	_ "time/tzdata"

	"github.com/fprambs/transferly/internal/app"
)

// main runs the command line and exits with status 1 on error.
func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	err := app.Run(ctx, os.Args[1:], os.Getenv, os.Stdout)
	stop()
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}
