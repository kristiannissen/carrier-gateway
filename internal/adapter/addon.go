// Package adapter provides interfaces and shared types for carrier integrations.
// This file is located at /internal/adapter/addon.go.
package adapter

// hasAddOn reports whether the given add-on type is present in the list.
func hasAddOn(addOns []AddOn, t AddOnType) bool {
	for _, a := range addOns {
		if a.Type == t {
			return true
		}
	}
	return false
}

// getAddOn returns the first AddOn of the given type, or a zero value if absent.
func getAddOn(addOns []AddOn, t AddOnType) (AddOn, bool) {
	for _, a := range addOns {
		if a.Type == t {
			return a, true
		}
	}
	return AddOn{}, false
}
