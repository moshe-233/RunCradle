package cli

import (
	"context"
	"encoding/json"
	"flag"
	"io"
	"os"
	"runcradle/internal/httpapi"
	"runcradle/internal/output"
	"runcradle/internal/webui"
	"time"
)

func serve(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("serve", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	workspace := flags.String("workspace", "", "")
	port := flags.Int("port", 0, "")
	binary := flags.String("process-compose", "", "")
	if e := flags.Parse(args); e != nil {
		return failure(stdout, 2, "arguments", e.Error())
	}
	if *workspace == "" || flags.NArg() != 0 {
		return failure(stdout, 2, "arguments", "--workspace required; no positional arguments")
	}
	resolved, e := resolveEngine(ctx, *binary, flagSet(flags, "process-compose"))
	if e != nil {
		return failure(stdout, engineResolutionCode(ctx, e), "engine", e.Error())
	}
	helper, e := os.Executable()
	if e != nil {
		return failure(stdout, 125, "host", e.Error())
	}
	server, e := httpapi.Open(httpapi.Config{Workspace: *workspace, Binary: resolved.Path, Helper: helper, Port: *port, Page: webui.Handler()})
	if e != nil {
		return failure(stdout, 2, "host", e.Error())
	}
	sinks, e := output.New(stdout, stderr, 2*time.Second)
	if e != nil {
		server.Close()
		return 125
	}
	defer sinks.Close()
	raw, e := json.Marshal(server.Ready())
	if e == nil {
		_, e = sinks.Out.WriteContext(context.Background(), append(raw, '\n'))
	}
	if e != nil {
		server.Close()
		return 125
	}
	code := 0
	select {
	case <-ctx.Done():
		code = CancellationCode(ctx)
	case e = <-server.Done():
		if e != nil {
			code = 125
		}
	}
	if e = server.Close(); e != nil {
		return 125
	}
	return code
}
