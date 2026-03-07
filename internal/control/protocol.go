package control

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"os"
	"time"
)

type Handler interface {
	Handle(context.Context, Request) Response
}

func writeMessage(w io.Writer, value any) error {
	encoder := json.NewEncoder(w)
	return encoder.Encode(value)
}

func readRequest(r *bufio.Reader) (Request, error) {
	var req Request
	if err := json.NewDecoder(r).Decode(&req); err != nil {
		return Request{}, err
	}
	return req, nil
}

func readResponse(r *bufio.Reader) (Response, error) {
	var resp Response
	if err := json.NewDecoder(r).Decode(&resp); err != nil {
		return Response{}, err
	}
	return resp, nil
}

func Listen(network, address string) (net.Listener, error) {
	if network == "npipe" {
		return listenNamedPipe(address)
	}
	if network == "unix" {
		_ = os.Remove(address)
		listener, err := net.Listen(network, address)
		if err != nil {
			return nil, err
		}
		_ = os.Chmod(address, 0o600)
		return listener, nil
	}
	return net.Listen(network, address)
}

func Serve(ctx context.Context, listener net.Listener, handler Handler) error {
	go func() {
		<-ctx.Done()
		_ = listener.Close()
	}()

	for {
		conn, err := listener.Accept()
		if err != nil {
			if errors.Is(err, net.ErrClosed) || ctx.Err() != nil {
				return nil
			}
			if temporary, ok := err.(interface{ Temporary() bool }); ok && temporary.Temporary() {
				continue
			}
			return err
		}
		go handleConnection(ctx, conn, handler)
	}
}

func handleConnection(ctx context.Context, conn net.Conn, handler Handler) {
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(30 * time.Second))

	req, err := readRequest(bufio.NewReader(conn))
	if err != nil {
		_ = writeMessage(conn, Errorf("decode request: %v", err))
		return
	}
	resp := handler.Handle(ctx, req)
	_ = writeMessage(conn, resp)
}
