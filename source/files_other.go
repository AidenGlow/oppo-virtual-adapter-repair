//go:build !windows

package main

import (
	"io"
	"os"
)

func checkDLLPath(path string) error { return ordinary(path) }
func renameNoReplace(source, target string) error {
	if err := os.Link(source, target); err != nil {
		return err
	}
	return os.Remove(source)
}
func copyNewFile(source, target string) error {
	in, err := os.Open(source)
	if err != nil {
		return err
	}
	defer in.Close()
	info, err := in.Stat()
	if err != nil {
		return err
	}
	out, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, info.Mode().Perm())
	if err != nil {
		return err
	}
	_, err = io.Copy(out, in)
	closeErr := out.Close()
	if err != nil {
		os.Remove(target)
		return err
	}
	return closeErr
}
func replaceNative(target, temp string) error { return os.Rename(temp, target) }

type yamlFileGuard struct{ lock, source *os.File }

func openYAMLGuard(path string) (*yamlFileGuard, error) {
	lock, err := os.OpenFile(path+".oppo-repair.lock", os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return nil, err
	}
	g := &yamlFileGuard{lock: lock}
	g.source, err = os.Open(path)
	if err != nil {
		g.Close()
		return nil, err
	}
	return g, nil
}
func (g *yamlFileGuard) closeSource() {
	if g.source != nil {
		g.source.Close()
		g.source = nil
	}
}
func (g *yamlFileGuard) Close() {
	g.closeSource()
	if g.lock != nil {
		name := g.lock.Name()
		g.lock.Close()
		g.lock = nil
		os.Remove(name)
	}
}

func createReceiptFile(source, target string) (*os.File, error) {
	info, err := os.Stat(source)
	if err != nil {
		return nil, err
	}
	return os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, info.Mode().Perm())
}
