//go:build !windows

package main

func checkRuntime() error        { return nil }
func showStartupError(err error) {}
