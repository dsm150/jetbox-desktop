//go:build !windows

package flow

import "os"

// renameFile вне Windows — обычное атомарное переименование.
func renameFile(from, to string) error { return os.Rename(from, to) }
