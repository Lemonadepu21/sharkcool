//go:build linux

package main

func prepareApplication() func() { return func() {} }
func showStartupError(err error) {}
