package main

// Much of the core of this is copied from go's cover tool itself.

// Copyright 2013 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// The rest is written by Dustin Sallings

import (
	"bytes"
	"fmt"
	"go/build"
	"io/ioutil"
	"log"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/tools/cover"
)

func getModuleNameFromGoMod(goModPath string) string {
	bytes, err := ioutil.ReadFile(goModPath)
	if err != nil {
		log.Printf("Couldn't read go.mod %v", err)
	}
	contents := string(bytes)
	lines := strings.Split(contents, "\n")
	for _, line := range lines {
		if strings.HasPrefix(line, "module") {
			fields := strings.Fields(line)
			if len(fields) > 1 {
				return fields[1]
			}
		}
	}
	return ""
}

func maybeFindFileFastPath(file string) string {
	// Look to see if there is a go.mod.
	abs, err := filepath.Abs(".")
	if err != nil {
		log.Printf("Failed to get abs: %v", err)
		return ""
	}
	var moduleName string
	for {
		goModFile := filepath.Join(abs, "go.mod")
		info, err := os.Stat(goModFile)
		if err == nil && !info.IsDir() {
			moduleName = getModuleNameFromGoMod(goModFile)
			break
		}
		d := filepath.Dir(abs)
		if len(d) >= len(abs) {
			log.Printf("Ran out %q %q", d, abs)
			return ""
		}
		abs = d
	}

	if moduleName == "" {
		log.Println("No module name")
		return ""
	}
	joined := filepath.Join(abs, strings.TrimPrefix(file, moduleName))
	if fileInfo, err := os.Stat(joined); err == nil && !fileInfo.IsDir() {
		return joined
	}
	return ""
}

func findFile(file string) (string, error) {
	if fileName := maybeFindFileFastPath(file); fileName != "" {
		return fileName, nil
	}
	log.Printf("Couldn't find fast path for file %s", file)
	dir, file := filepath.Split(file)
	pkg, err := build.Import(dir, ".", build.FindOnly)
	if err != nil {
		return "", fmt.Errorf("can't find %q: %v", file, err)
	}
	return filepath.Join(pkg.Dir, file), nil
}

// mergeProfs merges profiles for same target packages.
// It assumes each profiles have same sorted FileName and Blocks.
func mergeProfs(pfss [][]*cover.Profile) []*cover.Profile {
	// skip empty profiles ([no test files])
	for i := 0; i < len(pfss); i++ {
		if len(pfss[i]) > 0 {
			pfss = pfss[i:]
			break
		}
	}
	if len(pfss) == 0 {
		return nil
	} else if len(pfss) == 1 {
		return pfss[0]
	}
	head, rest := pfss[0], pfss[1:]
	ret := make([]*cover.Profile, 0, len(head))
	for i, profile := range head {
		for _, ps := range rest {
			// find profiles
			if len(ps) == 0 {
				continue
			} else if len(ps) < i+1 {
				continue
			} else if ps[i].FileName != profile.FileName {
				continue
			}
			profile.Blocks = mergeProfBlocks(profile.Blocks, ps[i].Blocks)
		}
		ret = append(ret, profile)
	}
	return ret
}

func mergeProfBlocks(as, bs []cover.ProfileBlock) []cover.ProfileBlock {
	if len(as) != len(bs) {
		log.Fatal("Two block length should be same")
	}
	// cover.ProfileBlock genereated by cover.ParseProfiles() is sorted by
	// StartLine and StartCol, so we can use index.
	ret := make([]cover.ProfileBlock, 0, len(as))
	for i, a := range as {
		b := bs[i]
		if a.StartLine != b.StartLine || a.StartCol != b.StartCol {
			log.Fatal("Blocks are not sorted")
		}
		a.Count += b.Count
		ret = append(ret, a)
	}
	return ret
}

// toSF converts profiles to sourcefiles for coveralls.
func toSF(profs []*cover.Profile) ([]*SourceFile, error) {
	var rv []*SourceFile
	for _, prof := range profs {
		path, err := findFile(prof.FileName)
		if err != nil {
			log.Fatalf("Can't find %v", err)
		}
		fb, err := ioutil.ReadFile(path)
		if err != nil {
			log.Fatalf("Error reading %v: %v", path, err)
		}
		sf := &SourceFile{
			Name:     getCoverallsSourceFileName(path),
			Source:   string(fb),
			Coverage: make([]interface{}, 1+bytes.Count(fb, []byte{'\n'})),
		}

		for _, block := range prof.Blocks {
			for i := block.StartLine; i <= block.EndLine; i++ {
				count, _ := sf.Coverage[i-1].(int)
				sf.Coverage[i-1] = count + block.Count
			}
		}

		rv = append(rv, sf)
	}

	return rv, nil
}

func parseCover(fn string) ([]*SourceFile, error) {
	var pfss [][]*cover.Profile
	fmt.Println("Parsing profiles")
	for _, p := range strings.Split(fn, ",") {
		profs, err := cover.ParseProfiles(p)
		if err != nil {
			return nil, fmt.Errorf("Error parsing coverage: %v", err)
		}
		pfss = append(pfss, profs)
	}
	fmt.Println("Parsed profiles")

	mergedProfs := mergeProfs(pfss)
	fmt.Println("Merged profiles")
	sourceFiles, err := toSF(mergedProfs)
	if err != nil {
		return nil, err
	}
	fmt.Println("convered merged profiles to sf")

	return sourceFiles, nil
}
