//go:build linux

// Standalone BRB02 library consumer: no web server, desktop integration,
// temperature collector, official binaries, or upstream checkout.
package main

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"time"

	"sharkcool-linux/brb02"
)

func main() {
	transport := flag.String("transport", "auto", "auto, ble, usb")
	address := flag.String("address", "", "paired BRB02 MAC")
	device := flag.String("device", "", "BRB02 hidraw path")
	flag.Parse()
	if err := probe(brb02.Options{Transport: *transport, Address: *address, Device: *device}); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func probe(options brb02.Options) error {
	c, err := brb02.NewWithOptions(options, false)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	done := make(chan struct{})
	go func() { c.Run(ctx); close(done) }()
	defer func() { cancel(); <-done }()
	tick := time.NewTicker(50 * time.Millisecond)
	defer tick.Stop()
	for !c.Snapshot().Connected {
		select {
		case <-ctx.Done():
			return fmt.Errorf("connection: %s", c.Snapshot().Error)
		case <-tick.C:
		}
	}
	firmware, err := c.Query(ctx, "firmware")
	if err != nil {
		return err
	}
	lighting, err := c.GetRGB(ctx)
	if err != nil {
		return err
	}
	return json.NewEncoder(os.Stdout).Encode(map[string]any{"device": c.Snapshot().Device, "firmwareHex": hex.EncodeToString(firmware.Payload), "lighting": lighting})
}
