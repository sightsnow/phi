package control

import (
	"bufio"
	"context"
	"encoding/json"
	"time"
)

type Client struct {
	Network string
	Address string
	Timeout time.Duration
}

func NewClient(network, address string) *Client {
	return &Client{
		Network: network,
		Address: address,
		Timeout: 2 * time.Second,
	}
}

func (c *Client) Send(ctx context.Context, req Request) (Response, error) {
	conn, err := dialControl(ctx, c.Network, c.Address, c.Timeout)
	if err != nil {
		return Response{}, err
	}
	defer conn.Close()

	_ = conn.SetDeadline(time.Now().Add(c.Timeout))
	if err := writeMessage(conn, req); err != nil {
		return Response{}, err
	}
	return readResponse(bufio.NewReader(conn))
}

func (c *Client) Call(ctx context.Context, action string, payload any, out any) error {
	req, err := NewRequest(action, payload)
	if err != nil {
		return err
	}
	resp, err := c.Send(ctx, req)
	if err != nil {
		return err
	}
	if !resp.OK {
		return &RemoteError{Message: resp.Error}
	}
	if out == nil || len(resp.Payload) == 0 {
		return nil
	}
	return json.Unmarshal(resp.Payload, out)
}
