package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/leoli-dev/agent-post/internal/adminui"
	"github.com/leoli-dev/agent-post/internal/cli"
	"github.com/leoli-dev/agent-post/internal/client"
	"github.com/leoli-dev/agent-post/internal/mcp"
	"github.com/leoli-dev/agent-post/internal/server"
)

var version = "dev"

func main() { os.Exit(run()) }
func run() int {
	args := os.Args[1:]
	if len(args) == 0 || args[0] == "help" || args[0] == "--help" || args[0] == "-h" {
		fmt.Fprint(os.Stdout, cli.Help)
		return 0
	}
	if args[0] == "version" {
		fmt.Fprintf(os.Stdout, "{\"name\":\"agent-post\",\"version\":%q}\n", version)
		return 0
	}
	if args[0] == "serve" {
		if err := serve(args[1:]); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		return 0
	}
	path, args, err := cli.ExtractConfig(args)
	if err != nil {
		return cli.Failure(os.Stdout, os.Stderr, err)
	}
	isMCP := len(args) > 0 && args[0] == "mcp"
	fail := func(err error) int {
		if isMCP {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		return cli.Failure(os.Stdout, os.Stderr, err)
	}
	config, err := client.LoadConfig(path)
	if err != nil {
		return fail(err)
	}
	c, err := client.New(config)
	if err != nil {
		return fail(err)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if isMCP {
		if len(args) != 1 {
			return fail(errors.New("mcp takes only the global --config option"))
		}
		if err = mcp.Run(ctx, c, version); err != nil && ctx.Err() == nil {
			return fail(err)
		}
		return 0
	}
	return cli.Run(ctx, c, args, os.Stdout, os.Stderr)
}
func serve(args []string) error {
	f := flag.NewFlagSet("serve", flag.ContinueOnError)
	f.SetOutput(os.Stderr)
	data := f.String("data-dir", "agent-post-data", "service data directory")
	listen := f.String("listen", "127.0.0.1:8080", "HTTP listen address")
	secure := f.Bool("secure-cookies", false, "require HTTPS for administrator cookies")
	if err := f.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if f.NArg() != 0 {
		return errors.New("unexpected serve arguments")
	}
	s, err := server.Open(server.Options{DataDir: *data, AdminPassword: os.Getenv("AGENT_POST_ADMIN_PASSWORD"), SecureCookies: *secure, AdminHandler: adminui.Handler()})
	if err != nil {
		return err
	}
	defer s.Close()
	listener, err := net.Listen("tcp", *listen)
	if err != nil {
		return err
	}
	httpServer := &http.Server{Handler: s.Handler(), ReadHeaderTimeout: 10 * time.Second, ReadTimeout: 2 * time.Minute, WriteTimeout: 2 * time.Minute, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 32 << 10}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	result := make(chan error, 1)
	go func() { result <- httpServer.Serve(listener) }()
	fmt.Fprintf(os.Stderr, "Agent Post listening on %s\n", listener.Addr())
	select {
	case err = <-result:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-ctx.Done():
		shutdown, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if err = httpServer.Shutdown(shutdown); err != nil {
			httpServer.Close()
			return err
		}
		return nil
	}
}
