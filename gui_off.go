//go:build nogui

package main

import "errors"

func runGUI() error {
	return errors.New("this build has no desktop UI; run `hidane serve`")
}
