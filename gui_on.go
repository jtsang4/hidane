//go:build !nogui

package main

import (
	"github.com/jtsang4/hidane/internal/config"
	"github.com/jtsang4/hidane/internal/desktop"
)

func runGUI() error { return desktop.Run(config.Load()) }
