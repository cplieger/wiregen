package wiregen

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"

	"github.com/cplieger/atomicfile/v4"
)

const roleOpenAPI = "openapi"

// genFile is one output. Its name is inside the TS directory for a TS file,
// else the path it is written at with its symlinks resolved.
type genFile struct {
	role, name, content string
	ts                  bool
}

// outputs lists every file this call writes, content empty. It refuses a TS
// path that is absolute, leaves the TS directory or reaches out of it
// through a symlink, and two outputs that are one file, before anything is
// created.
func (g *generator) outputs() ([]genFile, error) {
	out := []genFile{
		{role: "types", name: g.o.typesFilename, ts: true},
		{role: "decoders", name: g.o.decodersFilename, ts: true},
		{role: "validators", name: g.o.validatorsFilename, ts: true},
	}
	for _, opt := range []struct {
		role, name string
		on         bool
	}{
		{"events", g.o.eventsFilename, len(g.Events) > 0},
		{"constants", g.o.constantsFilename, len(g.Constants) > 0},
		{"client", g.o.clientFilename, len(g.Endpoints) > 0},
		{"arbitraries", g.o.arbitrariesFilename, g.o.arbitrariesFilename != ""},
	} {
		if opt.on {
			out = append(out, genFile{role: opt.role, name: opt.name, ts: true})
		}
	}
	for _, p := range g.o.openAPIFiles {
		out = append(out, genFile{role: roleOpenAPI, name: p})
	}
	tsDir, err := locate(g.o.tsDir)
	if err != nil {
		return nil, fmt.Errorf("wiregen: the TS directory %q: %w", g.o.tsDir, err)
	}
	var root *os.Root
	if tsDir.anchors[len(tsDir.anchors)-1].rest == "" {
		if root, err = os.OpenRoot(g.o.tsDir); err != nil {
			return nil, fmt.Errorf("wiregen: open %s: %w", g.o.tsDir, err)
		}
		defer root.Close()
	}
	claimed := make([]target, 0, len(out))
	for i := range out {
		f := &out[i]
		t, err := f.locate(root, tsDir)
		if err != nil {
			return nil, err
		}
		if j := slices.IndexFunc(claimed, t.same); j >= 0 {
			return nil, fmt.Errorf("wiregen: the %s and %s files %q and %q are one file", out[j].role, f.role, out[j].name, f.name)
		}
		claimed = append(claimed, t)
	}
	return out, nil
}

// locate finds f's target, naming f in an error: a TS file below tsDir,
// through root when tsDir exists, and any other at its resolved path, which
// becomes f's name.
func (f *genFile) locate(root *os.Root, tsDir target) (target, error) {
	if !f.ts {
		if f.name == "" {
			return target{}, errors.New("wiregen: WithOpenAPIFiles names an empty path")
		}
		t, err := locate(f.name)
		if err != nil {
			return target{}, fmt.Errorf("wiregen: the %s file %q: %w", f.role, f.name, err)
		}
		f.name = t.path
		return t, nil
	}
	if !filepath.IsLocal(f.name) {
		return target{}, fmt.Errorf("wiregen: the %s file %q must be a relative path inside the TS directory", f.role, f.name)
	}
	f.name = filepath.Clean(f.name)
	if root == nil {
		return tsDir.below(splitPath(f.name)), nil
	}
	t, err := locateIn(root, f.name)
	if err != nil {
		return target{}, fmt.Errorf("wiregen: the %s file %q: %w", f.role, f.name, err)
	}
	return t, nil
}

// target is where an output lands, as seen from every existing file or
// directory on its path: each anchor holds one, and the slash-separated
// names from it to the target.
type target struct {
	path    string // the path with its symlinks resolved; ambient targets only
	anchors []anchor
}

type anchor struct {
	fi   fs.FileInfo
	rest string
}

// same reports whether t and u are one file: from one file or directory,
// compared by device and inode, they name the same rest. rest is compared
// without case, as a case-insensitive file system (macOS's default) names
// it, so whether two outputs collide never depends on which already exist.
func (t target) same(u target) bool {
	for _, a := range t.anchors {
		for _, b := range u.anchors {
			if os.SameFile(a.fi, b.fi) && strings.EqualFold(a.rest, b.rest) {
				return true
			}
		}
	}
	return false
}

// walk builds the target of names from base, Stat-ing each prefix with
// stat until one does not exist. It returns how many names exist.
func walk(base fs.FileInfo, names []string, stat func(i int) (fs.FileInfo, error)) (target, int, error) {
	fis := []fs.FileInfo{base}
	i := 0
	for ; i < len(names); i++ {
		fi, err := stat(i)
		if errors.Is(err, fs.ErrNotExist) {
			break
		}
		if err != nil {
			return target{}, 0, err
		}
		fis = append(fis, fi)
	}
	t := target{anchors: make([]anchor, len(fis))}
	for j, fi := range fis {
		t.anchors[j] = anchor{fi: fi, rest: strings.Join(names[j:], "/")}
	}
	return t, i, nil
}

// below returns the target of names under t, which names a directory.
func (t target) below(names []string) target {
	u := target{anchors: make([]anchor, len(t.anchors))}
	for i, a := range t.anchors {
		u.anchors[i] = anchor{fi: a.fi, rest: path.Join(append([]string{a.rest}, names...)...)}
	}
	return u
}

// locate finds p's target. The names p holds past the part that exists may
// not climb with "..", which the file system cannot resolve there.
func locate(p string) (target, error) {
	prefix := "."
	if filepath.IsAbs(p) {
		prefix = filepath.VolumeName(p) + string(filepath.Separator)
	}
	base, err := os.Stat(prefix)
	if err != nil {
		return target{}, err
	}
	names := splitPath(p)
	// The names are joined unresolved, never through filepath.Join's
	// lexical clean, so the operating system resolves every symlink and "..".
	sep := string(filepath.Separator)
	join := func(n int) string { return strings.TrimSuffix(prefix, sep) + sep + strings.Join(names[:n], sep) }
	t, n, err := walk(base, names, func(i int) (fs.FileInfo, error) { return os.Stat(join(i + 1)) })
	if err != nil {
		return target{}, err
	}
	if slices.Contains(names[n:], "..") {
		return target{}, errors.New("climbs with \"..\" out of a directory that does not exist")
	}
	resolved, err := filepath.EvalSymlinks(join(n))
	if err != nil {
		return target{}, err
	}
	t.path = filepath.Join(append([]string{resolved}, names[n:]...)...)
	return t, nil
}

// locateIn finds the target of name, local and clean, below root; root
// refuses a symlink that leaves it.
func locateIn(root *os.Root, name string) (target, error) {
	base, err := root.Stat(".")
	if err != nil {
		return target{}, err
	}
	names := splitPath(name)
	t, _, err := walk(base, names, func(i int) (fs.FileInfo, error) {
		return root.Stat(filepath.Join(names[:i+1]...))
	})
	return t, err
}

func splitPath(p string) []string {
	return slices.DeleteFunc(strings.Split(filepath.ToSlash(p), "/"), func(n string) bool { return n == "" || n == "." })
}

// writeFiles stages every file as an atomicfile pending file, the TS files
// through an os.Root of tsDir, then commits each.
func writeFiles(ctx context.Context, tsDir string, files []genFile) error {
	if err := os.MkdirAll(tsDir, 0o755); err != nil {
		return fmt.Errorf("wiregen: create %s: %w", tsDir, err)
	}
	root, err := os.OpenRoot(tsDir)
	if err != nil {
		return fmt.Errorf("wiregen: open %s: %w", tsDir, err)
	}
	defer root.Close()
	pending := make([]*atomicfile.PendingFile, 0, len(files))
	defer func() {
		for _, p := range pending {
			_ = p.Cleanup() // a no-op once committed; a temp is best-effort beyond that
		}
	}()
	for _, f := range files {
		var p *atomicfile.PendingFile
		if f.ts {
			p, err = atomicfile.NewPendingFileInRoot(ctx, root, f.name, atomicfile.WithMkdirMode(0o755))
		} else {
			p, err = atomicfile.NewPendingFile(ctx, f.name, atomicfile.WithMkdirMode(0o755))
		}
		if err != nil {
			return fmt.Errorf("wiregen: stage the %s file %s: %w", f.role, f.name, err)
		}
		pending = append(pending, p)
		if _, err := p.WriteString(f.content); err != nil {
			return fmt.Errorf("wiregen: write the %s file %s: %w", f.role, f.name, err)
		}
	}
	for i, p := range pending {
		if _, err := p.Commit(ctx); err != nil {
			return fmt.Errorf("wiregen: write the %s file %s: %w", files[i].role, files[i].name, err)
		}
	}
	return nil
}
