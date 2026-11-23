package library

import "os"

// Test exports for series_folder_test.go (package library_test).

var IsCrossDevice = isCrossDevice
var CopyDirThenRemove = copyDirThenRemove

// SwapSeriesFolderRenamer replaces the directory renamer; returns the previous function.
func SwapSeriesFolderRenamer(fn func(oldpath, newpath string) error) func(oldpath, newpath string) error {
	prev := seriesFolderRenamer
	if fn == nil {
		seriesFolderRenamer = os.Rename
	} else {
		seriesFolderRenamer = fn
	}
	return prev
}
