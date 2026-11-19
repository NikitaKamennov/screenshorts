//go:build !windows && !linux

package platform

import "context"

func RememberFocus(context.Context) FocusTarget { return nil }
