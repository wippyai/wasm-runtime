package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/wippyai/wasm-runtime/component"
	"github.com/wippyai/wasm-runtime/runtime"
	"github.com/wippyai/wasm-runtime/wasi/preview2"
)

func main() {
	var (
		wasmFile    = flag.String("wasm", "", "Path to component wasm file")
		funcName    = flag.String("func", "", "Function to call (optional)")
		strArg      = flag.String("arg", "", "String argument to pass")
		envVars     = flag.String("env", "", "Environment variables (KEY=VAL,KEY2=VAL2)")
		cliArgs     = flag.String("argv", "", "CLI arguments (comma-separated)")
		preopens    = flag.String("preopens", "", "Preopened directories (/host:/guest,/host2:/guest2)")
		stdin       = flag.String("stdin", "", "Stdin data")
		list        = flag.Bool("list", false, "List exported functions and exit")
		interactive = flag.Bool("i", false, "Interactive mode with TUI")
	)
	flag.Parse()

	if *wasmFile == "" {
		fmt.Fprintln(os.Stderr, "Usage: run -wasm <file.wasm> [-func name] [-arg string] [-env K=V,...]")
		fmt.Fprintln(os.Stderr, "       run -wasm <file.wasm> -list")
		fmt.Fprintln(os.Stderr, "       run -wasm <file.wasm> -i  (interactive mode)")
		os.Exit(1)
	}

	if *interactive {
		if err := runInteractive(*wasmFile); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		return
	}

	if err := run(*wasmFile, *funcName, *strArg, *envVars, *cliArgs, *preopens, *stdin, *list); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func run(wasmFile, funcName, strArg, envStr, argvStr, preopensStr, stdinStr string, listOnly bool) error {
	ctx := context.Background()

	// Read component
	data, err := os.ReadFile(wasmFile)
	if err != nil {
		return fmt.Errorf("read file: %w", err)
	}

	// Validate component
	validated, err := component.DecodeAndValidate(data)
	if err != nil {
		return fmt.Errorf("decode: %w", err)
	}

	// Show component info
	fmt.Printf("Component: %s\n", wasmFile)
	fmt.Printf("Core modules: %d\n", len(validated.Raw.CoreModules))
	fmt.Printf("Imports: %d\n", len(validated.Raw.Imports))
	fmt.Printf("Exports: %d\n", len(validated.Raw.Exports))

	// List exported functions
	resolver := component.NewTypeResolverWithInstances(
		validated.Raw.TypeIndexSpace,
		validated.Raw.InstanceTypes,
	)
	reg, err := component.NewCanonRegistry(validated.Raw, resolver)
	if err != nil {
		return fmt.Errorf("registry: %w", err)
	}

	fmt.Printf("\nExported functions:\n")
	var exportedFuncs []string
	for name, lift := range reg.Lifts {
		exportedFuncs = append(exportedFuncs, name)
		var params []string
		for i, p := range lift.Params {
			pname := fmt.Sprintf("arg%d", i)
			if i < len(lift.ParamNames) && lift.ParamNames[i] != "" {
				pname = lift.ParamNames[i]
			}
			params = append(params, pname+": "+fmt.Sprintf("%T", p))
		}
		result := ""
		if len(lift.Results) > 0 {
			result = " -> " + fmt.Sprintf("%T", lift.Results[0])
		}
		fmt.Printf("  %s(%s)%s\n", name, strings.Join(params, ", "), result)
	}

	if listOnly {
		return nil
	}

	// Create runtime
	rt, err := runtime.New(ctx)
	if err != nil {
		return fmt.Errorf("create runtime: %w", err)
	}
	defer rt.Close(ctx)

	// Create WASI configuration
	wasi := preview2.New()
	defer wasi.Close()

	// Configure environment
	if envStr != "" {
		env := make(map[string]string)
		for _, kv := range strings.Split(envStr, ",") {
			parts := strings.SplitN(kv, "=", 2)
			if len(parts) == 2 {
				env[parts[0]] = parts[1]
			}
		}
		wasi.WithEnv(env)
	}

	// Configure CLI args
	if argvStr != "" {
		wasi.WithArgs(strings.Split(argvStr, ","))
	}

	// Configure preopens
	if preopensStr != "" {
		preops := make(map[string]string)
		for _, mapping := range strings.Split(preopensStr, ",") {
			parts := strings.SplitN(mapping, ":", 2)
			if len(parts) == 2 {
				preops[parts[0]] = parts[1]
			}
		}
		wasi.WithPreopens(preops)
	}

	// Configure stdin
	if stdinStr != "" {
		wasi.WithStdin([]byte(stdinStr))
	}

	// Register WASI
	if err := rt.RegisterWASI(wasi); err != nil {
		return fmt.Errorf("register WASI: %w", err)
	}

	// Load component
	module, err := rt.LoadComponent(ctx, data)
	if err != nil {
		return fmt.Errorf("load component: %w", err)
	}

	// Instantiate
	fmt.Printf("\nInstantiating component...\n")
	instance, err := module.Instantiate(ctx)
	if err != nil {
		return fmt.Errorf("instantiate: %w", err)
	}
	defer instance.Close(ctx)

	// If no function specified, try common entry points
	if funcName == "" {
		for _, name := range []string{"_start", "run", "main"} {
			for _, f := range exportedFuncs {
				if f == name {
					funcName = name
					break
				}
			}
			if funcName != "" {
				break
			}
		}
		if funcName == "" && len(exportedFuncs) == 1 {
			funcName = exportedFuncs[0]
		}
		if funcName == "" {
			fmt.Printf("\nNo function specified and no common entry point found.\n")
			fmt.Printf("Use -func to specify a function to call.\n")
			return nil
		}
	}

	// Call function
	fmt.Printf("\nCalling %s", funcName)
	var result any
	if strArg != "" {
		fmt.Printf("(%q)...\n", strArg)
		result, err = instance.Call(ctx, funcName, strArg)
	} else {
		fmt.Printf("()...\n")
		result, err = instance.Call(ctx, funcName)
	}

	if err != nil {
		return fmt.Errorf("call %s: %w", funcName, err)
	}

	fmt.Printf("Result: %v\n", result)

	// Print stdout/stderr if any
	stdout := wasi.Stdout()
	if len(stdout) > 0 {
		fmt.Printf("\n--- stdout ---\n%s", stdout)
	}
	stderr := wasi.Stderr()
	if len(stderr) > 0 {
		fmt.Printf("\n--- stderr ---\n%s", stderr)
	}

	return nil
}
