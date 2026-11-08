// Package brcap defines the capabilities modules share. Modules never import
// each other; one module provides an implementation of an interface here (with
// bridgr.Provide) and others look it up by type (bridgr.Lookup). In-memory
// implementations are included for tests, single-instance apps, and modules
// that need a fallback when no other module provides the capability.
//
// Changing an interface in this package is a breaking change for every module.
package brcap
