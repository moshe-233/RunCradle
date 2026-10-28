package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"runcradle/internal/cli"
	"runcradle/internal/nodeexec"
)

func main() {
	// Catch EPIPE for this CLI's bounded output. A caught handler is reset by
	// exec; SIG_IGN would leak into Process Compose and business programs.
	signal.Notify(make(chan os.Signal, 1), syscall.SIGPIPE)
	os.Exit(execute())
}
func execute() int {
	ctx, cancel := context.WithCancelCause(context.Background())
	defer cancel(nil)
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(signals)
	go func() {
		select {
		case s := <-signals:
			cancel(cli.SignalCause(s.(syscall.Signal)))
		case <-ctx.Done():
		}
	}()
	if len(os.Args) > 1 && os.Args[1] == "_exec" {
		return nodeexec.Run(ctx, os.Args[2:], os.Stdout, os.Stderr)
	}
	return cli.Run(ctx, os.Args[1:], os.Stdout, os.Stderr)
}
