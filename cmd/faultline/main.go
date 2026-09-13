package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"faultline/internal/config"
	"faultline/internal/control"
	"faultline/internal/control/admin"
	"faultline/internal/fault"
	httpproxy "faultline/internal/proxy/http"
	"faultline/internal/recorder"
)

func main() {
	if len(os.Args) > 1 && os.Args[1] == "serve" {
		// A closed event pipe must not terminate the proxy before recording the sink failure.
		signal.Ignore(syscall.SIGPIPE)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, os.Args[1:], os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	if len(args) == 0 || args[0] == "--help" || args[0] == "-h" {
		fmt.Fprintln(stdout, "Usage: faultline <validate|serve|reload|enable|disable|status> [flags]\n\nvalidate/serve/reload require --config FILE.\nserve supports --start-enabled, --admin-socket PATH and --event-buffer N.\nRuntime commands support --admin-socket PATH and --timeout DURATION.")
		return nil
	}
	command := args[0]
	if command != "validate" && command != "serve" && command != "reload" && command != "enable" && command != "disable" && command != "status" {
		return fmt.Errorf("unknown command %q; use --help", command)
	}
	flags := flag.NewFlagSet(command, flag.ContinueOnError)
	flags.SetOutput(stderr)
	var filename string
	if command == "serve" || command == "validate" || command == "reload" {
		flags.StringVar(&filename, "config", "", "root YAML configuration file")
	}
	socket := admin.DefaultSocket()
	if command != "validate" {
		flags.StringVar(&socket, "admin-socket", socket, "Unix socket identifying the local instance")
	}
	var enabled bool
	eventBuffer := recorder.DefaultBuffer
	timeout := 5 * time.Second
	if command == "serve" {
		flags.BoolVar(&enabled, "start-enabled", false, "enable fault injection at startup")
		flags.IntVar(&eventBuffer, "event-buffer", eventBuffer, "maximum queued JSON events")
	} else if command != "validate" {
		flags.DurationVar(&timeout, "timeout", timeout, "admin command timeout")
	}
	if err := flags.Parse(args[1:]); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if flags.NArg() != 0 || ((command == "serve" || command == "validate" || command == "reload") && filename == "") {
		return errors.New("--config FILE is required; positional arguments are unsupported")
	}
	if socket == "" || eventBuffer <= 0 || timeout <= 0 {
		return errors.New("admin socket must be nonempty; timeout and event buffer must be positive")
	}
	if command != "validate" && command != "serve" {
		if command == "reload" {
			absolute, err := filepath.Abs(filename)
			if err != nil {
				return err
			}
			filename = absolute
		}
		data, err := admin.Call(ctx, socket, command, filename, timeout)
		if err != nil {
			return err
		}
		_, err = stdout.Write(data)
		return err
	}
	document, err := config.Load(filename)
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
	records := recorder.New(stdout, eventBuffer)
	defer func() {
		flush, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		err := records.Close(flush)
		counts := records.Counters()
		if err != nil || counts.Dropped != 0 || counts.WriteErrors != 0 || counts.Pending != 0 {
			fmt.Fprintf(stderr, "Recorder events incomplete: dropped=%d write_errors=%d pending=%d flush_error=%v\n", counts.Dropped, counts.WriteErrors, counts.Pending, err)
		}
	}()
	server, err := httpproxy.Start(service, httpproxy.Options{Executor: fault.Builtin{}, Recorder: records})
	if err != nil {
		return err
	}
	defer server.Close()
	management, err := admin.Start(socket, service, records, server.Listeners)
	if err != nil {
		return err
	}
	defer func() {
		management.Close()
		server.Close()
		counts := records.Counters()
		records.Record(recorder.Event{Info: service.Acquire().Info(), Type: "control", Operation: "shutdown", Outcome: "stopped", Counters: &counts})
	}()
	records.Record(recorder.Event{Info: service.Acquire().Info(), Type: "control", Operation: "serve", Outcome: "ready"})
	fmt.Fprintf(stderr, "Listeners ready; injection_enabled=%t; admin_socket=%s (application/upstream readiness is not checked)\n", enabled, socket)
	select {
	case <-ctx.Done():
		return nil
	case err := <-server.Errors():
		return err
	case err := <-management.Errors():
		return err
	}
}
