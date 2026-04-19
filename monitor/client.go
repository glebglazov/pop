package monitor

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net"
	"time"
)

const clientTimeout = 2 * time.Second

// SendRequest dials the daemon TCP address, sends req, and returns the response.
func SendRequest(addr string, req Request) (Response, error) {
	conn, err := net.DialTimeout("tcp", addr, clientTimeout)
	if err != nil {
		return Response{}, fmt.Errorf("connect to daemon: %w", err)
	}
	defer conn.Close()

	conn.SetDeadline(time.Now().Add(clientTimeout))

	data, err := json.Marshal(req)
	if err != nil {
		return Response{}, fmt.Errorf("marshal request: %w", err)
	}
	data = append(data, '\n')

	if _, err := conn.Write(data); err != nil {
		return Response{}, fmt.Errorf("write request: %w", err)
	}

	scanner := bufio.NewScanner(conn)
	if !scanner.Scan() {
		if err := scanner.Err(); err != nil {
			return Response{}, fmt.Errorf("read response: %w", err)
		}
		return Response{}, fmt.Errorf("read response: connection closed")
	}

	var resp Response
	if err := json.Unmarshal(scanner.Bytes(), &resp); err != nil {
		return Response{}, fmt.Errorf("unmarshal response: %w", err)
	}
	return resp, nil
}
