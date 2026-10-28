// Package fuseadapter translates native FUSE requests to fsapi through a
// restricted disposable direct-I/O adapter. Normal publication, writable mmap
// and native compatibility remain subject to the documented platform gates.
package fuseadapter
