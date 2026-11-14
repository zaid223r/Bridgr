package bridgr

import "github.com/zaid223r/Bridgr/core/brcrud"

// Filters maps a JSON field name to the operators list requests may filter it
// with; fields that aren't listed can't be filtered. Set it in
// CRUDOptions.Filters:
//
//	Filters: bridgr.Filters{
//		"user_id":    bridgr.Exact,                   // ?user_id=7
//		"title":      bridgr.Exact | bridgr.Contains, // ?title=x, ?title[contains]=x
//		"created_at": bridgr.Range,                   // ?created_at[gte]=2026-01-01T00:00:00Z
//	},
type Filters = brcrud.Filters

// FilterOps is a set of filter operators; combine them with |.
type FilterOps = brcrud.FilterOps

// Filter operator sets; see brcrud.Exact and the others for the query forms.
const (
	Exact    = brcrud.Exact
	Not      = brcrud.Not
	Contains = brcrud.Contains
	Range    = brcrud.Range
	In       = brcrud.In
	All      = brcrud.All
)
