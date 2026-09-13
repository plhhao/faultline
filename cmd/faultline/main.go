package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"

	"faultline/internal/config"
	"faultline/internal/control"
	"faultline/internal/fault"
	httpproxy "faultline/internal/proxy/http"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, os.Args[1:], os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	if len(args) == 0 || args[0] == "--help" || args[0] == "-h" {
		fmt.Fprintln(stdout, "Usage: faultline <validate|serve> --config FILE\n\nserve supports --start-enabled to inject configured faults at startup.")
		return nil
	}
	command := args[0]
	if command != "validate" && command != "serve" {
		return fmt.Errorf("unknown command %q; use --help", command)
	}
	flags := flag.NewFlagSet(command, flag.ContinueOnError)
	flags.SetOutput(stderr)
	filename := flags.String("config", "", "root YAML configuration file")
	var enabled bool
	if command == "serve" {
		flags.BoolVar(&enabled, "start-enabled", false, "enable fault injection at startup")
	}
	if err := flags.Parse(args[1:]); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if *filename == "" || flags.NArg() != 0 {
		return errors.New("--config FILE is required; positional arguments are unsupported")
	}
	document, err := config.Load(*filename)
	if err != nil {
		return err
	}
	if command == "validate" {
		fmt.Fprintln(stdout, "Configuration valid")
		return nil
	}
	service, err := control.New(document)
	if err != nil {
		return err
	}
	service.SetEnabled(enabled)
	server, err := httpproxy.Start(service, httpproxy.Options{Executor: fault.Builtin{}})
	if err != nil {
		return err
	}
	defer server.Close()
	fmt.Fprintf(stdout, "Listeners ready; injection_enabled=%t (application/upstream readiness is not checked)\n", enabled)
	select {
	case <-ctx.Done():
		return nil
	case err := <-server.Errors():
		return err
	}
}
