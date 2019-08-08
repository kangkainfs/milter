[![GoDoc](https://godoc.org/github.com/phalaaxx/milter?status.svg)](https://godoc.org/github.com/phalaaxx/milter)

# milter
A Go library for milter support heavily inspired from https://github.com/andybalholm/milter
For example how to use the library see https://github.com/phalaaxx/pf-milters - postfix milter for email classification with bogofilter and blacklisting messages which contain files with executable extensions.

# CHANGES

I changed the original package for better usage. 

````golang

import "github.com/x-mod/milter"

import (
	"context"
	"net"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"github.com/x-mod/milter"
	"github.com/x-mod/routine"
	"github.com/x-mod/tcpserver"
)

func Main(cmd *cobra.Command, args []string) error {
	srv := tcpserver.NewServer(
		tcpserver.Network(viper.GetString("inet")),
		tcpserver.Address(viper.GetString("address")),
		tcpserver.Handler(Handler),
	)
	return routine.Main(
		routine.ExecutorFunc(srv.Serve),
		routine.Interrupts(routine.DefaultCancelInterruptors...),
		routine.Cleanup(
			routine.ExecutorFunc(func(ctx context.Context) error {
				srv.Close()
				return nil
			})),
	)
}

func Handler(ctx context.Context, conn net.Conn) error {
	ssn := milter.NewMilterSession(conn, milter.WithMilter(&MyMilter{}), milter.WithContext(ctx))
	return ssn.Serve()
}

//MyMilter implement the Milter interface
type MyMilter struct{}

///TODO
````