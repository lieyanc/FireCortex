package managedinfra

import (
	"fmt"
	"net"
)

func checkLoopbackPort(port int) error {
	ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		return fmt.Errorf("port 127.0.0.1:%d is already in use: %w", port, err)
	}
	_ = ln.Close()
	return nil
}
