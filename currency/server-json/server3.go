package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net"
	"os"
	"time"

	curr "github.com/vladimirvivien/go-networking/tcp/curlib"
)

var (
	currencies = curr.Load("./data.csv")
)

// This program implements a simple currency lookup service
// over TCP or Unix Data Socket. It loads ISO currency
// information using package curlib (see above) and makes
// and serves it using JSON-enocoded data.
//
// Clients send currency search requests as JSON objects such
// as {"Get":"USD"}. The request data is then unmarshalled to Go
// type curr.CurrencyRequest{Get:"USD"} using the encoding/json
// package.
//
// The request is then used to search the list of
// currencies. The search result, a []curr.Currency, is marshalled
// to JSON array of objects and send to the client.
//
// Configure Connection:
// This version of the server highlights the configuration of
// the connection to set read and write deadline for the client.
// If those deadlines are reached, the server will drop the connection.
//
// Usage: server [options]
// options:
//   -e host endpoint, default ":4040"
//   -n network protocol [tcp,unix], default "tcp"
func main() {
	// setup flags
	var addr string
	var network string
	flag.StringVar(&addr, "e", ":4040", "service endpoint [ip addr or socket path]")
	flag.StringVar(&network, "n", "tcp", "network protocol [tcp,unix]")
	flag.Parse()

	// validate supported network protocols
	switch network {
	case "tcp", "tcp4", "tcp6", "unix":
	default:
		fmt.Println("unsupported network protocol")
		os.Exit(1)
	}

	// create a listener for provided network and host address
	ln, err := net.Listen(network, addr)
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
	defer ln.Close()
	fmt.Println("**** Global Currency Service ***")
	fmt.Printf("Service started: (%s) %s\n", network, addr)

	// connection loop
	for {
		conn, err := ln.Accept()
		if err != nil {
			fmt.Println(err)
			conn.Close()
			continue
		}
		fmt.Println("Connected to ", conn.RemoteAddr())
		go handleConnection(conn)
	}
}

// handle client connection
func handleConnection(conn net.Conn) {
	defer conn.Close()

	// set initial deadline prior to entering
	// the client request/response loop to 90 seconds.
	// This means that the client has 90 seconds to send
	// its initial request or loose the connection.
	if err := conn.SetDeadline(time.Now().Add(time.Second * 90)); err != nil {
		fmt.Println("failed to set deadline:", err)
		return
	}

	// loop to keep connection alive until client breaks connection
	for {
		// The following call uses the JSON encoder support for
		// Go's IO streaming API (io.Reader).
		dec := json.NewDecoder(conn)

		// Next, the decoder blocks waiting for incoming data.
		// As data comes from client, it streams it from net.Conn,
		// which implements io.Reader, and decodes the incoming data
		// into Go value curr.CurrencyRequest
		var req curr.CurrencyRequest
		if err := dec.Decode(&req); err != nil {
			// json.Decode() could return decoding err,
			// io err, or networking err.  This makes error handling
			// a little more complex.

			// handle error based on error type
			switch err := err.(type) {
			//network error: disconnect
			case net.Error:
				// depending on requirements, the timeout can be
				// renewed or subsequently rejected.
				if err.Timeout() {
					fmt.Println("deadline reached, disconnecting...")
				}
				// dont continue, break connection
				fmt.Println("network error:", err)
				return

			//other errors: send error info to client, then continue
			default:
				if err == io.EOF {
					fmt.Println("closing connection:", err)
					return
				}
				// encode curr.CurrencyError to send to client
				enc := json.NewEncoder(conn)
				if err := enc.Encode(&curr.CurrencyError{Error: err.Error()}); err != nil {
					// if encoding fails, just stop
					fmt.Println("failed error encoding:", err)
					return
				}
				continue
			}
		}

		// search currencies, result is []curr.Currency
		result := curr.Find(currencies, req.Get)

		// marshal result to JSON array
		enc := json.NewEncoder(conn)
		if err := enc.Encode(&result); err != nil {
			switch err := err.(type) {
			case net.Error:
				fmt.Println("failed to send response:", err)
				return
			default:
				enc := json.NewEncoder(conn)
				if err := enc.Encode(&curr.CurrencyError{Error: err.Error()}); err != nil {
					fmt.Println("failed to send error:", err)
					return
				}
				continue
			}
		}

		// renew deadline for anther 90 secs
		if err := conn.SetDeadline(time.Now().Add(time.Second * 90)); err != nil {
			fmt.Println("failed to set deadline:", err)
			return
		}
	}
}
