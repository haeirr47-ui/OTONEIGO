//go:build !windows

package resourcecache

// Other platform ports can supply a native free-space probe. The cache still
// bounds every individual download and reserves its declared size for pruning.
func freeBytes(root string) (uint64, bool) { return 0, false }
