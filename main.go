// PUBLIC DOMAIN NOTICE
// National Center for Biotechnology Information
//
// This software/database is a "United States Government Work" under the
// terms of the United States Copyright Act.  It was written as part of
// the author's official duties as a United States Government employee and
// thus cannot be copyrighted.  This software/database is freely available
// to the public for use. The National Library of Medicine and the U.S.
// Government have not placed any restriction on its use or reproduction.
//
// Although all reasonable efforts have been taken to ensure the accuracy
// and reliability of the software and data, the NLM and the U.S.
// Government do not and cannot warrant the performance or results that
// may be obtained by using this software or data. The NLM and the U.S.
// Government disclaim all warranties, express or implied, including
// warranties of performance, merchantability or fitness for any particular
// purpose.
//
// Please cite the author in any work or product based on this material.

package main

import (
	"context"
	"crypto/tls"
	"fmt"
	"github.com/urfave/cli"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	hv1 "google.golang.org/grpc/health/grpc_health_v1"
	"os"
	"time"
)

// version variable is set during compilation using ldflags
var version string

const (
	// ExitCodeUsage is returned if application used incorrectly
	ExitCodeUsage = 1
	// ExitCodeHealthCheckNegative is returned if health status is not SERVING
	ExitCodeHealthCheckNegative = 2
	// ExitCodeUnexpected is returned if any other error happens
	ExitCodeUnexpected = 127
)

// appFlags holds flags passed to application
type appFlags struct {
	timeout     time.Duration
	noFail      bool
	tls         bool
	tlsInsecure bool
	tlsCert     string
}

// appConfig holds processed application config
type appConfig struct {
	timeout       time.Duration
	noFail        bool
	serverAddress string
	serviceName   string
	creds         credentials.TransportCredentials
}

// mainFn is main application business logic
type mainFn func(config *appConfig) *cli.ExitError

func createApp(mainFn mainFn) *cli.App {
	app := cli.NewApp()
	flags := &appFlags{}

	app.Name = "gprobe"
	app.Usage = "universal gRPC health-checker. See https://github.com/grpc/grpc/blob/master/doc/health-checking.md"
	app.UsageText = "gprobe [options] server_address [service_name]"
	app.Version = version
	app.HideHelp = true
	app.OnUsageError = func(context *cli.Context, err error, isSubcommand bool) error {
		cli.ShowAppHelp(context)
		return cli.NewExitError(err.Error(), ExitCodeUsage)
	}
	cli.VersionPrinter = func(c *cli.Context) {
		fmt.Fprintf(c.App.Writer, "%s\n", c.App.Version)
	}
	app.Flags = []cli.Flag{
		cli.DurationFlag{
			Name:        "timeout, t",
			Usage:       "Operation timeout",
			Destination: &flags.timeout,
			Value:       1 * time.Second,
		},
		cli.BoolFlag{
			Name:        "no-fail, n",
			Usage:       "Do not fail if service status is other than SERVING. Note: this has no effect on server check",
			Destination: &flags.noFail,
		},
		cli.BoolFlag{
			Name:        "tls",
			Usage:       "Use TLS, verify server with CA certificates installed on this host",
			Destination: &flags.tls,
		},
		cli.BoolFlag{
			Name:        "tls-insecure",
			Usage:       "Use TLS, do NOT verify server (accept any certificate)",
			Destination: &flags.tlsInsecure,
		},
		cli.StringFlag{
			Name:        "tls-cert",
			Usage:       "Use TLS, verify server with specified certificate",
			Destination: &flags.tlsCert,
		},
	}
	app.Action = func(c *cli.Context) error {
		appConfig, err := createConfig(flags, c.Args())
		if err != nil {
			return c.App.OnUsageError(c, err, false)
		}
		// Pass all input to mainFn
		return mainFn(appConfig)
	}
	return app
}

func createConfig(flags *appFlags, args cli.Args) (config *appConfig, err error) {
	config = &appConfig{}
	switch len(args) {
	case 2:
		config.serviceName = args.Get(1)
		config.serverAddress = args.Get(0)
		break
	case 1:
		config.serverAddress = args.Get(0)
		break
	default:
		return nil, fmt.Errorf("exactly 1 to 2 arguments are required")
	}
	creds, err := parseCredentials(flags)
	if err != nil {
		return nil, err
	}
	config.creds = creds
	config.timeout = flags.timeout
	config.noFail = flags.noFail
	return
}

func parseCredentials(flags *appFlags) (creds credentials.TransportCredentials, err error) {
	tlsFlagsSet := 0
	if flags.tls {
		creds = credentials.NewTLS(&tls.Config{})
		tlsFlagsSet++
	}
	if flags.tlsInsecure {
		creds = credentials.NewTLS(&tls.Config{InsecureSkipVerify: true})
		tlsFlagsSet++
	}
	if len(flags.tlsCert) > 0 {
		creds, err = credentials.NewClientTLSFromFile(flags.tlsCert, "")
		tlsFlagsSet++
	}
	// explicitly disallow passing more than one --tls option
	if tlsFlagsSet > 1 {
		creds = nil
		err = fmt.Errorf("at most one of --tls, --tls-insecure and --tls-cert should be provided")
	}
	return
}

func main() {
	createApp(appMain).Run(os.Args)
}

func appMain(config *appConfig) *cli.ExitError {
	ctx, cancel := context.WithTimeout(context.Background(), config.timeout)
	defer cancel()

	connection, err := connect(ctx, config.serverAddress, config.creds)
	if err != nil {
		return cli.NewExitError(err.Error(), ExitCodeUnexpected)
	}
	defer connection.Close()

	status, err := check(ctx, connection, config.serviceName)
	if err != nil {
		return cli.NewExitError(err.Error(), ExitCodeUnexpected)
	}

	fmt.Fprintln(os.Stdout, status.String())
	if !(config.noFail || status == hv1.HealthCheckResponse_SERVING) {
		return cli.NewExitError("health-check failed", ExitCodeHealthCheckNegative)
	}

	// for some reason returning nil here causes err == nil to be false in urfave/cli/errors.go:79
	return cli.NewExitError("", 0)
}

func connect(ctx context.Context, serverAddress string, creds credentials.TransportCredentials) (connection *grpc.ClientConn, err error) {
	var dialOption grpc.DialOption
	if creds == nil {
		dialOption = grpc.WithInsecure()
	} else {
		dialOption = grpc.WithTransportCredentials(creds)
	}
	connection, err = grpc.DialContext(ctx, serverAddress, dialOption)
	return
}

func check(ctx context.Context, connection *grpc.ClientConn, service string) (status hv1.HealthCheckResponse_ServingStatus, err error) {
	client := hv1.NewHealthClient(connection)
	response, err := client.Check(ctx, &hv1.HealthCheckRequest{
		Service: service,
	})
	if response != nil {
		status = response.Status
	}
	return
}
