package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"runcradle/internal/buildinfo"
	"runcradle/internal/processcompose"
)

func flagSet(flags *flag.FlagSet, name string) bool {
	set := false
	flags.Visit(func(f *flag.Flag) {
		if f.Name == name {
			set = true
		}
	})
	return set
}
func resolveEngine(ctx context.Context, explicit string, explicitSet bool) (processcompose.ResolvedBinary, error) {
	executable, err := os.Executable()
	if err != nil {
		return processcompose.ResolvedBinary{}, fmt.Errorf("%w: %v", processcompose.ErrInternal, err)
	}
	cwd, err := os.Getwd()
	if err != nil {
		return processcompose.ResolvedBinary{}, fmt.Errorf("%w: %v", processcompose.ErrInternal, err)
	}
	return processcompose.ResolveBinary(ctx, processcompose.ResolveOptions{Explicit: explicit, ExplicitSet: explicitSet, Executable: executable, WorkingDir: cwd, SearchPath: os.Getenv("PATH"), RequireBundle: buildinfo.Distribution == "bundle", ProductVersion: buildinfo.Version, ProductCommit: buildinfo.Commit})
}
func engineResolutionCode(ctx context.Context, err error) int {
	if ctx.Err() != nil {
		return CancellationCode(ctx)
	}
	if errors.Is(err, processcompose.ErrInternal) {
		return 125
	}
	return 2
}
