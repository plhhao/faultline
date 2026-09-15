package main

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"flag"
	"fmt"
	"log"
	"net"
	"os"
	"time"

	"faultline/examples/grpc/unary"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/protobuf/types/known/wrapperspb"
)

func main() {
	mode := flag.String("mode", "server", "server or client")
	address := flag.String("address", "127.0.0.1:9000", "listen address or client target")
	cert := flag.String("cert", "", "TLS certificate PEM")
	key := flag.String("key", "", "TLS key PEM")
	ca := flag.String("ca", "", "server CA for client; enables TLS")
	clientCA := flag.String("client-ca", "", "client CA for server; requires mTLS")
	size := flag.Int("size", 256<<10, "unary payload bytes")
	timeout := flag.Duration("timeout", 5*time.Second, "client deadline")
	flag.Parse()
	tc := &tls.Config{MinVersion: tls.VersionTLS12}
	if *cert != "" || *key != "" {
		pair, err := tls.LoadX509KeyPair(*cert, *key)
		if err != nil {
			log.Fatal(err)
		}
		tc.Certificates = []tls.Certificate{pair}
	}
	pool := func(path string) *x509.CertPool {
		data, err := os.ReadFile(path)
		if err != nil {
			log.Fatal(err)
		}
		p := x509.NewCertPool()
		if !p.AppendCertsFromPEM(data) {
			log.Fatal("invalid CA")
		}
		return p
	}
	if *mode == "server" {
		var opts []grpc.ServerOption
		if *clientCA != "" {
			tc.ClientCAs = pool(*clientCA)
			tc.ClientAuth = tls.RequireAndVerifyClientCert
		}
		if len(tc.Certificates) > 0 {
			opts = append(opts, grpc.Creds(credentials.NewTLS(tc)))
		} else if *clientCA != "" {
			log.Fatal("client-ca requires cert/key")
		}
		server := grpc.NewServer(opts...)
		unary.Register(server, unary.Echo{})
		listener, err := net.Listen("tcp", *address)
		if err != nil {
			log.Fatal(err)
		}
		log.Printf("unary Echo listening on %s", listener.Addr())
		log.Fatal(server.Serve(listener))
	}
	if *mode != "client" || *size < 0 {
		log.Fatal("invalid mode or size")
	}
	var creds credentials.TransportCredentials = insecure.NewCredentials()
	if *ca != "" {
		tc.RootCAs = pool(*ca)
		creds = credentials.NewTLS(tc)
	} else if len(tc.Certificates) > 0 {
		log.Fatal("client cert requires ca")
	}
	conn, err := grpc.NewClient(*address, grpc.WithTransportCredentials(creds), grpc.WithDisableRetry())
	if err != nil {
		log.Fatal(err)
	}
	defer conn.Close()
	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()
	payload := bytes.Repeat([]byte("x"), *size)
	response := new(wrapperspb.BytesValue)
	var header, trailer metadata.MD
	started := time.Now()
	err = conn.Invoke(ctx, unary.Method, wrapperspb.Bytes(payload), response, grpc.Header(&header), grpc.Trailer(&trailer))
	fmt.Printf("bytes=%d elapsed=%s header=%v trailer=%v error=%v\n", len(response.Value), time.Since(started), header, trailer, err)
	if err != nil {
		os.Exit(1)
	}
	if !bytes.Equal(payload, response.Value) {
		log.Fatal("payload mismatch")
	}
}
