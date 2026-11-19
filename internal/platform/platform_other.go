//go:build !windows && !linux

package platform

import "fmt"

func New(Actions) (Service, error) { return nil, fmt.Errorf("Поддержаны Windows и Linux") }
