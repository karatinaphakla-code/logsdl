package main

import (
	"os"
	"path/filepath"
)

// appRoot is the directory containing logsdl.exe (or the binary when go run).
func appRoot() string {
	exe, err := os.Executable()
	if err != nil {
		wd, _ := os.Getwd()
		if wd == "" {
			return "."
		}
		return wd
	}
	return filepath.Dir(exe)
}

func initAppRoot() string {
	root := appRoot()
	_ = os.Chdir(root)
	return root
}

func defaultDownloadsDir(root string) string {
	return filepath.Join(root, "downloads")
}
