package platform

import "context"

// FocusTarget is transient: no titles, contents or window IDs enter settings,
// logs or screenshot metadata. Restoration always respects the window manager.
type FocusTarget interface { Restore(context.Context) error }
