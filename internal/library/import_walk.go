package library

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

func listAllFilesUnder(absRoot string) ([]string, error) {
	const maxImportWalkFiles = 250_000
	var files []string
	err := filepath.WalkDir(absRoot, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		name := d.Name()
		if strings.HasPrefix(name, ".") {
			return nil
		}
		if len(files) >= maxImportWalkFiles {
			return fmt.Errorf("%w: import root has more than %d files (cap)", ErrInvalid, maxImportWalkFiles)
		}
		files = append(files, path)
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(files)
	return files, nil
}

// seriesDirsForRoot returns cleaned SeriesDir paths for every series on rootID.
