package labrunner

import (
	"debug/elf"
	"errors"
	"io"
	"slices"
	"strings"
)

// verifyPrivateEtcdBinary rejects implicit package-library relocation. Only a
// static native executable or the pinned FreeBSD base ABI libraries are allowed;
// dependencies on global /usr/local libraries/RPATH are never adopted.
func verifyPrivateEtcdBinary(file, digest string) error {
	if err := verifyExecutable(file, digest); err != nil {
		return err
	}
	f, err := elf.Open(file)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	if f.Class != elf.ELFCLASS64 || f.Machine != elf.EM_X86_64 || f.OSABI != elf.ELFOSABI_FREEBSD || f.Type != elf.ET_EXEC && f.Type != elf.ET_DYN {
		return errors.New("private etcd requires native FreeBSD amd64 ELF")
	}
	libraries, err := f.ImportedLibraries()
	if err != nil {
		return err
	}
	for _, library := range libraries {
		if !slices.Contains([]string{"libc.so.7", "libthr.so.3", "libm.so.5"}, library) {
			return errors.New("private etcd package-library relocation is unsupported; no global-library fallback")
		}
	}
	for _, tag := range []elf.DynTag{elf.DT_RPATH, elf.DT_RUNPATH} {
		paths, err := f.DynString(tag)
		if err != nil {
			return err
		}
		if len(paths) != 0 {
			return errors.New("private etcd ELF search paths must not select ambient libraries")
		}
	}
	for _, program := range f.Progs {
		if program.Type == elf.PT_INTERP {
			data, err := io.ReadAll(io.LimitReader(program.Open(), 512))
			if err != nil {
				return err
			}
			if strings.TrimRight(string(data), "\x00") != "/libexec/ld-elf.so.1" {
				return errors.New("private etcd ELF interpreter is not the FreeBSD base loader")
			}
		}
	}
	return nil
}
