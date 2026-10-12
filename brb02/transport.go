package brb02

import "context"

// Transport exchanges BRB02 reports. Writes receive a zero report ID followed
// by 64 bytes; USB sends this report, while BLE sends only its contained frame.
// Read must unblock when ctx is cancelled; Close releases only this connection.
type Transport interface {
	Read(context.Context, []byte) (int, error)
	Write([]byte) (int, error)
	Close() error
}

// Connector opens a transport. The returned endpoint identifies the connection,
// e.g. /dev/hidraw3 or ble://MAC. Run calls it again after a disconnection.
type Connector func(context.Context) (Transport, string, error)

type Options struct {
	Transport string
	Device    string
	Address   string
}

// NewWithConnector uses a caller-supplied backend without OS or UI dependencies.
// A nil connector is permitted for demo mode only; otherwise Run reports an error.
func NewWithConnector(connector Connector, demo bool) *Controller {
	return &Controller{demo: demo, connector: connector, status: Status{Demo: demo}}
}
