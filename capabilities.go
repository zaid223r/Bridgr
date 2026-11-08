package bridgr

import (
	"fmt"
	"reflect"
)

// Provide makes c the app's implementation of the capability interface C, such
// as brcap.KV or brcap.Scheduler, so other modules can find it with Lookup.
// It returns an error if C is not an interface type, if C is already provided
// or if the app has started. Call it from a module's Register.
func Provide[C any](a *App, c C) error {
	t := reflect.TypeFor[C]()
	if t.Kind() != reflect.Interface {
		return fmt.Errorf("bridgr: Provide[%s]: capabilities must be interface types", t)
	}
	if any(c) == nil {
		return fmt.Errorf("bridgr: Provide[%s]: nil implementation", t)
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.started {
		return fmt.Errorf("bridgr: Provide[%s]: the app has already started", t)
	}
	if a.capabilities == nil {
		a.capabilities = map[reflect.Type]any{}
	}
	if _, dup := a.capabilities[t]; dup {
		return fmt.Errorf("bridgr: Provide[%s]: already provided by another module", t)
	}
	a.capabilities[t] = c
	return nil
}

// Lookup returns the app's implementation of capability C, if a module
// provided one. Modules usually look capabilities up in Start, after every
// module has registered, and fall back to the in-memory versions in brcap.
func Lookup[C any](a *App) (C, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	c, ok := a.capabilities[reflect.TypeFor[C]()].(C)
	return c, ok
}
