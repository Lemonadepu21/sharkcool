//go:build linux

package brb02

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

// New is the legacy USB constructor. NewWithOptions enables either channel.
func New(path string, demo bool) *Controller {
	return NewWithConnector(func(ctx context.Context) (Transport, string, error) { return Open(path) }, demo)
}

func NewWithOptions(options Options, demo bool) (*Controller, error) {
	if options.Transport == "" {
		options.Transport = "auto"
	}
	if options.Transport != "usb" && options.Transport != "ble" && options.Transport != "auto" {
		return nil, fmt.Errorf("transport 必须为 auto、usb 或 ble")
	}
	c := NewWithConnector(nil, demo)
	c.connector = func(ctx context.Context) (Transport, string, error) {
		if options.Transport == "usb" {
			return Open(options.Device)
		}
		if options.Transport == "ble" {
			return OpenBLE(ctx, options.Address)
		}
		// Auto chooses an available endpoint; neither transport shares wire framing.
		if devices, err := DiscoverBLE(ctx); err == nil {
			for _, d := range devices {
				if d.ControlReady && d.Connected && (options.Address == "" || strings.EqualFold(options.Address, d.Address)) {
					if ble, blePath, bleErr := OpenBLE(ctx, d.Address); bleErr == nil {
						return ble, blePath, nil
					}
					break
				}
			}
		}
		usb, usbPath, usbErr := Open(options.Device)
		if usbErr == nil {
			return usb, usbPath, nil
		}
		ble, blePath, bleErr := OpenBLE(ctx, options.Address)
		if bleErr == nil {
			return ble, blePath, nil
		}
		return nil, "", errors.Join(fmt.Errorf("USB: %w", usbErr), fmt.Errorf("BLE: %w", bleErr))
	}
	return c, nil
}

// OpenWithOptions opens one transport without controller or UI dependencies.
func OpenWithOptions(ctx context.Context, options Options) (Transport, string, error) {
	c, err := NewWithOptions(options, false)
	if err != nil {
		return nil, "", err
	}
	return c.connector(ctx)
}
