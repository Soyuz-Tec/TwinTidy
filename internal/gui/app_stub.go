//go:build !windows

package gui

import (
	"errors"
)

var ErrUnsupportedPlatform = errors.New("TwinTidy provides a Windows-native desktop GUI and cannot run on this platform")

func Run() error { return ErrUnsupportedPlatform }

func SmokeTest() error { return ErrUnsupportedPlatform }
