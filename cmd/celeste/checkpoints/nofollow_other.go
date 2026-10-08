//go:build !unix

package checkpoints

// oNoFollow: no O_NOFOLLOW here. Entries with a root (all new ones) go
// through os.Root, which keeps every access inside it.
const oNoFollow = 0
