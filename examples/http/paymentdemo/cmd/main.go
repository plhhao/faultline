package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/plhhao/faultline/examples/http/paymentdemo"
)

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	if err := run(ctx, os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string) error {
	if len(args) == 0 || (args[0] != "serve" && args[0] != "run") {
		return errors.New("usage: paymentdemo serve [--listen ADDRESS] | run [--proxy URL --upstream URL --idempotent --timeout DURATION]")
	}
	flags := flag.NewFlagSet(args[0], flag.ContinueOnError)
	if args[0] == "run" {
		proxy := flags.String("proxy", "http://127.0.0.1:8080", "Faultline URL")
		upstream := flags.String("upstream", "http://127.0.0.1:9000", "direct payment dependency URL")
		idempotent := flags.Bool("idempotent", false, "reuse an Idempotency-Key across attempts")
		timeout := flags.Duration("timeout", 200*time.Millisecond, "timeout for each attempt")
		if err := flags.Parse(args[1:]); err != nil {
			return err
		}
		if flags.NArg() != 0 || *timeout <= 0 {
			return errors.New("positive timeout required; positional arguments unsupported")
		}
		result, err := paymentdemo.Run(ctx, *proxy, *upstream, *idempotent, *timeout)
		if writeErr := json.NewEncoder(os.Stdout).Encode(result); writeErr != nil {
			return writeErr
		}
		return err
	}
	listen := flags.String("listen", "127.0.0.1:9000", "payment listener")
	if err := flags.Parse(args[1:]); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return errors.New("positional arguments unsupported")
	}
	server := &http.Server{Addr: *listen, Handler: paymentdemo.NewService(), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 5 * time.Second, WriteTimeout: 5 * time.Second, IdleTimeout: 5 * time.Second}
	done := make(chan error, 1)
	go func() { done <- server.ListenAndServe() }()
	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		return server.Close()
	}
}
