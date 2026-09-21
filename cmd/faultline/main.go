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
	"faultline/internal/control/remote"
	"faultline/internal/fault"
	httpproxy "faultline/internal/proxy/http"
	"faultline/internal/proxy/mysql"
	"faultline/internal/proxy/postgresql"
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
		fmt.Fprintln(stdout, "Usage: faultline <validate|serve|reload|enable|disable|status|user|configure> [flags]\n\nvalidate/serve/reload require --config FILE.\nserve supports --start-enabled, --admin-socket PATH and --event-buffer N.\nRuntime commands support --admin-socket PATH and --timeout DURATION.\nManaged API/UI: serve --data-dir DIR --api-listen HOST:PORT --api-cert PEM --api-key PEM.\nAccounts: user --data-dir DIR --name NAME --role viewer|editor (password from stdin), or --delete.\nOffline infrastructure: configure --data-dir DIR --config FILE (instance must be stopped).")
		return nil
	}
	command := args[0]
	if command == "configure" {
		return runConfigure(args[1:], stdout, stderr)
	}
	if command == "user" {
		return runUser(args[1:], stdout, stderr)
	}
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
	var dataDir, apiAddress, apiCert, apiKey string
	eventBuffer := recorder.DefaultBuffer
	timeout := 5 * time.Second
	if command == "serve" {
		flags.StringVar(&dataDir, "data-dir", "", "managed API/UI data directory (0700); file mode when omitted")
		flags.StringVar(&apiAddress, "api-listen", "127.0.0.1:8443", "HTTPS admin/UI listener in managed mode")
		flags.StringVar(&apiCert, "api-cert", "", "HTTPS admin certificate PEM")
		flags.StringVar(&apiKey, "api-key", "", "HTTPS admin private key PEM")
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
	var document *config.Document
	var managed *remote.Server
	var service *control.Service
	var err error
	if command == "serve" && dataDir != "" {
		if apiCert == "" || apiKey == "" || apiAddress == "" {
			return errors.New("managed mode requires --api-cert, --api-key and --api-listen")
		}
		store, doc, openErr := remote.Open(dataDir, filename)
		if openErr != nil {
			return openErr
		}
		defer store.Close()
		document = doc
		managed, err = remote.New(store, document)
		if err != nil {
			return err
		}
		service = managed.Service()
	} else {
		document, err = config.Load(filename)
		if err != nil {
			return err
		}
	}
	if command == "validate" {
		fmt.Fprintln(stdout, "Configuration valid")
		return nil
	}
	if service == nil {
		service, err = control.New(document)
		if err != nil {
			return err
		}
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
	server, err := httpproxy.Start(service, httpproxy.Options{Executor: fault.Builtin{}, Recorder: records, Diagnostics: stderr})
	if err != nil {
		return err
	}
	defer server.Close()
	pgServer, err := postgresql.Start(service, records)
	if err != nil {
		return err
	}
	defer pgServer.Close()
	myServer, err := mysql.Start(service, records)
	if err != nil {
		return err
	}
	defer myServer.Close()
	listeners := func() []control.ListenerStatus {
		return append(append(server.Listeners(), pgServer.Listeners()...), myServer.Listeners()...)
	}
	management, err := admin.Start(socket, service, records, listeners, managed != nil)
	if err != nil {
		return err
	}
	defer func() {
		management.Close()
		drain, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		myServer.Close()
		pgServer.Close()
		server.Shutdown(drain)
		counts := records.Counters()
		records.Record(recorder.Event{Info: service.Acquire().Info(), Type: "control", Operation: "shutdown", Outcome: "stopped", Counters: &counts})
	}()
	var apiErrors <-chan error
	if managed != nil {
		if err := managed.Start(remote.Options{Address: apiAddress, Certificate: apiCert, Key: apiKey}, records, listeners); err != nil {
			return err
		}
		defer managed.Close()
		apiErrors = managed.Errors()
		fmt.Fprintf(stderr, "Managed UI: https://%s; local admin is read-only\n", apiAddress)
	}
	records.Record(recorder.Event{Info: service.Acquire().Info(), Type: "control", Operation: "serve", Outcome: "ready"})
	fmt.Fprintf(stderr, "Listeners ready; injection_enabled=%t; admin_socket=%s (application/upstream readiness is not checked)\n", enabled, socket)
	select {
	case <-ctx.Done():
		return nil
	case err := <-myServer.Errors():
		return err
	case err := <-pgServer.Errors():
		return err
	case err := <-server.Errors():
		return err
	case err := <-apiErrors:
		return err
	case err := <-management.Errors():
		return err
	}
}
