package main

import "net"

// sdNotify sends state, such as "READY=1", to the service manager over the
// socket named by $NOTIFY_SOCKET, which systemd sets for a Type=notify
// service.  It does nothing when socket is empty.  See sd_notify(3).
func sdNotify(socket, state string) error {
	if socket == "" {
		return nil
	}
	// A name starting with @ is an abstract socket; net handles that.
	conn, err := net.DialUnix("unixgram", nil, &net.UnixAddr{Name: socket, Net: "unixgram"})
	if err != nil {
		return err
	}
	defer conn.Close()
	_, err = conn.Write([]byte(state))
	return err
}
