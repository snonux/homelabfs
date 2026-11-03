//go:build linux || freebsd

package fuseadapter

import "github.com/hanwen/go-fuse/v2/fuse"

// Lookup dispatches only after the one-time binding.
func (s *Shell) Lookup(cancel <-chan struct{}, header *fuse.InHeader, name string, out *fuse.EntryOut) (status fuse.Status) {
	if a := s.dispatch.Load(); a != nil {
		return a.Lookup(cancel, header, name, out)
	}
	return fuse.EACCES
}

// Forget dispatches only after the one-time binding.
func (s *Shell) Forget(nodeid, nlookup uint64) {
	if a := s.dispatch.Load(); a != nil {
		a.Forget(nodeid, nlookup)
		return
	}

}

// GetAttr dispatches only after the one-time binding.
func (s *Shell) GetAttr(cancel <-chan struct{}, input *fuse.GetAttrIn, out *fuse.AttrOut) (code fuse.Status) {
	if a := s.dispatch.Load(); a != nil {
		return a.GetAttr(cancel, input, out)
	}
	return fuse.EACCES
}

// SetAttr dispatches only after the one-time binding.
func (s *Shell) SetAttr(cancel <-chan struct{}, input *fuse.SetAttrIn, out *fuse.AttrOut) (code fuse.Status) {
	if a := s.dispatch.Load(); a != nil {
		return a.SetAttr(cancel, input, out)
	}
	return fuse.EACCES
}

// Mknod dispatches only after the one-time binding.
func (s *Shell) Mknod(cancel <-chan struct{}, input *fuse.MknodIn, name string, out *fuse.EntryOut) (code fuse.Status) {
	if a := s.dispatch.Load(); a != nil {
		return a.Mknod(cancel, input, name, out)
	}
	return fuse.EACCES
}

// Mkdir dispatches only after the one-time binding.
func (s *Shell) Mkdir(cancel <-chan struct{}, input *fuse.MkdirIn, name string, out *fuse.EntryOut) (code fuse.Status) {
	if a := s.dispatch.Load(); a != nil {
		return a.Mkdir(cancel, input, name, out)
	}
	return fuse.EACCES
}

// Unlink dispatches only after the one-time binding.
func (s *Shell) Unlink(cancel <-chan struct{}, header *fuse.InHeader, name string) (code fuse.Status) {
	if a := s.dispatch.Load(); a != nil {
		return a.Unlink(cancel, header, name)
	}
	return fuse.EACCES
}

// Rmdir dispatches only after the one-time binding.
func (s *Shell) Rmdir(cancel <-chan struct{}, header *fuse.InHeader, name string) (code fuse.Status) {
	if a := s.dispatch.Load(); a != nil {
		return a.Rmdir(cancel, header, name)
	}
	return fuse.EACCES
}

// Rename dispatches only after the one-time binding.
func (s *Shell) Rename(cancel <-chan struct{}, input *fuse.RenameIn, oldName string, newName string) (code fuse.Status) {
	if a := s.dispatch.Load(); a != nil {
		return a.Rename(cancel, input, oldName, newName)
	}
	return fuse.EACCES
}

// Link dispatches only after the one-time binding.
func (s *Shell) Link(cancel <-chan struct{}, input *fuse.LinkIn, filename string, out *fuse.EntryOut) (code fuse.Status) {
	if a := s.dispatch.Load(); a != nil {
		return a.Link(cancel, input, filename, out)
	}
	return fuse.EACCES
}

// Symlink dispatches only after the one-time binding.
func (s *Shell) Symlink(cancel <-chan struct{}, header *fuse.InHeader, pointedTo string, linkName string, out *fuse.EntryOut) (code fuse.Status) {
	if a := s.dispatch.Load(); a != nil {
		return a.Symlink(cancel, header, pointedTo, linkName, out)
	}
	return fuse.EACCES
}

// Readlink dispatches only after the one-time binding.
func (s *Shell) Readlink(cancel <-chan struct{}, header *fuse.InHeader) (out []byte, code fuse.Status) {
	if a := s.dispatch.Load(); a != nil {
		return a.Readlink(cancel, header)
	}
	return nil, fuse.EACCES
}

// Access dispatches only after the one-time binding.
func (s *Shell) Access(cancel <-chan struct{}, input *fuse.AccessIn) (code fuse.Status) {
	if a := s.dispatch.Load(); a != nil {
		return a.Access(cancel, input)
	}
	return fuse.EACCES
}

// GetXAttr dispatches only after the one-time binding.
func (s *Shell) GetXAttr(cancel <-chan struct{}, header *fuse.InHeader, attr string, dest []byte) (sz uint32, code fuse.Status) {
	if a := s.dispatch.Load(); a != nil {
		return a.GetXAttr(cancel, header, attr, dest)
	}
	return 0, fuse.EACCES
}

// ListXAttr dispatches only after the one-time binding.
func (s *Shell) ListXAttr(cancel <-chan struct{}, header *fuse.InHeader, dest []byte) (uint32, fuse.Status) {
	if a := s.dispatch.Load(); a != nil {
		return a.ListXAttr(cancel, header, dest)
	}
	return 0, fuse.EACCES
}

// SetXAttr dispatches only after the one-time binding.
func (s *Shell) SetXAttr(cancel <-chan struct{}, input *fuse.SetXAttrIn, attr string, data []byte) fuse.Status {
	if a := s.dispatch.Load(); a != nil {
		return a.SetXAttr(cancel, input, attr, data)
	}
	return fuse.EACCES
}

// RemoveXAttr dispatches only after the one-time binding.
func (s *Shell) RemoveXAttr(cancel <-chan struct{}, header *fuse.InHeader, attr string) (code fuse.Status) {
	if a := s.dispatch.Load(); a != nil {
		return a.RemoveXAttr(cancel, header, attr)
	}
	return fuse.EACCES
}

// Create dispatches only after the one-time binding.
func (s *Shell) Create(cancel <-chan struct{}, input *fuse.CreateIn, name string, out *fuse.CreateOut) (code fuse.Status) {
	if a := s.dispatch.Load(); a != nil {
		return a.Create(cancel, input, name, out)
	}
	return fuse.EACCES
}

// Open dispatches only after the one-time binding.
func (s *Shell) Open(cancel <-chan struct{}, input *fuse.OpenIn, out *fuse.OpenOut) (status fuse.Status) {
	if a := s.dispatch.Load(); a != nil {
		return a.Open(cancel, input, out)
	}
	return fuse.EACCES
}

// Read dispatches only after the one-time binding.
func (s *Shell) Read(cancel <-chan struct{}, input *fuse.ReadIn, buf []byte) (fuse.ReadResult, fuse.Status) {
	if a := s.dispatch.Load(); a != nil {
		return a.Read(cancel, input, buf)
	}
	return nil, fuse.EACCES
}

// Lseek dispatches only after the one-time binding.
func (s *Shell) Lseek(cancel <-chan struct{}, in *fuse.LseekIn, out *fuse.LseekOut) fuse.Status {
	if a := s.dispatch.Load(); a != nil {
		return a.Lseek(cancel, in, out)
	}
	return fuse.EACCES
}

// GetLk dispatches only after the one-time binding.
func (s *Shell) GetLk(cancel <-chan struct{}, input *fuse.LkIn, out *fuse.LkOut) (code fuse.Status) {
	if a := s.dispatch.Load(); a != nil {
		return a.GetLk(cancel, input, out)
	}
	return fuse.EACCES
}

// SetLk dispatches only after the one-time binding.
func (s *Shell) SetLk(cancel <-chan struct{}, input *fuse.LkIn) (code fuse.Status) {
	if a := s.dispatch.Load(); a != nil {
		return a.SetLk(cancel, input)
	}
	return fuse.EACCES
}

// SetLkw dispatches only after the one-time binding.
func (s *Shell) SetLkw(cancel <-chan struct{}, input *fuse.LkIn) (code fuse.Status) {
	if a := s.dispatch.Load(); a != nil {
		return a.SetLkw(cancel, input)
	}
	return fuse.EACCES
}

// Release dispatches only after the one-time binding.
func (s *Shell) Release(cancel <-chan struct{}, input *fuse.ReleaseIn) {
	if a := s.dispatch.Load(); a != nil {
		a.Release(cancel, input)
		return
	}

}

// Write dispatches only after the one-time binding.
func (s *Shell) Write(cancel <-chan struct{}, input *fuse.WriteIn, data []byte) (written uint32, code fuse.Status) {
	if a := s.dispatch.Load(); a != nil {
		return a.Write(cancel, input, data)
	}
	return 0, fuse.EACCES
}

// CopyFileRange dispatches only after the one-time binding.
func (s *Shell) CopyFileRange(cancel <-chan struct{}, input *fuse.CopyFileRangeIn) (written uint32, code fuse.Status) {
	if a := s.dispatch.Load(); a != nil {
		return a.CopyFileRange(cancel, input)
	}
	return 0, fuse.EACCES
}

// Ioctl dispatches only after the one-time binding.
func (s *Shell) Ioctl(cancel <-chan struct{}, input *fuse.IoctlIn, inbuf []byte, output *fuse.IoctlOut, outbuf []byte) (code fuse.Status) {
	if a := s.dispatch.Load(); a != nil {
		return a.Ioctl(cancel, input, inbuf, output, outbuf)
	}
	return fuse.EACCES
}

// Flush dispatches only after the one-time binding.
func (s *Shell) Flush(cancel <-chan struct{}, input *fuse.FlushIn) fuse.Status {
	if a := s.dispatch.Load(); a != nil {
		return a.Flush(cancel, input)
	}
	return fuse.EACCES
}

// Fsync dispatches only after the one-time binding.
func (s *Shell) Fsync(cancel <-chan struct{}, input *fuse.FsyncIn) (code fuse.Status) {
	if a := s.dispatch.Load(); a != nil {
		return a.Fsync(cancel, input)
	}
	return fuse.EACCES
}

// Fallocate dispatches only after the one-time binding.
func (s *Shell) Fallocate(cancel <-chan struct{}, input *fuse.FallocateIn) (code fuse.Status) {
	if a := s.dispatch.Load(); a != nil {
		return a.Fallocate(cancel, input)
	}
	return fuse.EACCES
}

// OpenDir dispatches only after the one-time binding.
func (s *Shell) OpenDir(cancel <-chan struct{}, input *fuse.OpenIn, out *fuse.OpenOut) (status fuse.Status) {
	if a := s.dispatch.Load(); a != nil {
		return a.OpenDir(cancel, input, out)
	}
	return fuse.EACCES
}

// ReadDir dispatches only after the one-time binding.
func (s *Shell) ReadDir(cancel <-chan struct{}, input *fuse.ReadIn, out *fuse.DirEntryList) fuse.Status {
	if a := s.dispatch.Load(); a != nil {
		return a.ReadDir(cancel, input, out)
	}
	return fuse.EACCES
}

// ReadDirPlus dispatches only after the one-time binding.
func (s *Shell) ReadDirPlus(cancel <-chan struct{}, input *fuse.ReadIn, out *fuse.DirEntryList) fuse.Status {
	if a := s.dispatch.Load(); a != nil {
		return a.ReadDirPlus(cancel, input, out)
	}
	return fuse.EACCES
}

// ReleaseDir dispatches only after the one-time binding.
func (s *Shell) ReleaseDir(input *fuse.ReleaseIn) {
	if a := s.dispatch.Load(); a != nil {
		a.ReleaseDir(input)
		return
	}

}

// FsyncDir dispatches only after the one-time binding.
func (s *Shell) FsyncDir(cancel <-chan struct{}, input *fuse.FsyncIn) (code fuse.Status) {
	if a := s.dispatch.Load(); a != nil {
		return a.FsyncDir(cancel, input)
	}
	return fuse.EACCES
}

// StatFs dispatches only after the one-time binding.
func (s *Shell) StatFs(cancel <-chan struct{}, input *fuse.InHeader, out *fuse.StatfsOut) (code fuse.Status) {
	if a := s.dispatch.Load(); a != nil {
		return a.StatFs(cancel, input, out)
	}
	return fuse.EACCES
}

// Statx dispatches only after the one-time binding.
func (s *Shell) Statx(cancel <-chan struct{}, input *fuse.StatxIn, out *fuse.StatxOut) (code fuse.Status) {
	if a := s.dispatch.Load(); a != nil {
		return a.Statx(cancel, input, out)
	}
	return fuse.EACCES
}
