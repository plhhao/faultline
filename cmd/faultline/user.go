package main

import (
	"errors"
	"github.com/plhhao/faultline/internal/control/remote"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
)

func runUser(args []string, stdout, stderr io.Writer) error {
	f := flag.NewFlagSet("user", flag.ContinueOnError)
	f.SetOutput(stderr)
	dir := f.String("data-dir", "", "private managed data directory")
	name := f.String("name", "", "account name")
	role := f.String("role", "viewer", "viewer or editor")
	remove := f.Bool("delete", false, "revoke account")
	if err := f.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if *dir == "" || *name == "" || f.NArg() != 0 {
		return errors.New("user requires --data-dir and --name")
	}
	password := ""
	if !*remove {
		info, err := os.Stdin.Stat()
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeCharDevice != 0 {
			return errors.New("read password securely into a shell variable and pipe it to stdin; see tester guide")
		}
		data, err := io.ReadAll(io.LimitReader(os.Stdin, 1026))
		if err != nil {
			return err
		}
		password = strings.TrimSuffix(strings.TrimSuffix(string(data), "\n"), "\r")
	}
	if err := remote.SetUser(*dir, *name, *role, password, *remove); err != nil {
		return err
	}
	fmt.Fprintln(stdout, "Account updated; existing sessions for this account are revoked.")
	return nil
}

func runConfigure(args []string, stdout, stderr io.Writer) error {
	f := flag.NewFlagSet("configure", flag.ContinueOnError)
	f.SetOutput(stderr)
	dir := f.String("data-dir", "", "private managed data directory")
	filename := f.String("config", "", "complete replacement config; instance must be stopped")
	if err := f.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if *dir == "" || *filename == "" || f.NArg() != 0 {
		return errors.New("configure requires --data-dir and --config")
	}
	if err := remote.Configure(*dir, *filename); err != nil {
		return err
	}
	fmt.Fprintln(stdout, "Managed config saved. Start the instance to use it; injection defaults to disabled.")
	return nil
}
