// Copyright 2009 The Ninep Authors.  All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package ufs

import (
	"flag"
	"fmt"
	"os"
	"path"
	"sync"

	"github.com/rminnich/ninep/next"
	"github.com/rminnich/ninep/rpc"
)

type File struct {
	rpc.QID
	fullName string
	File     *os.File
}

type FileServer struct {
	mu        *sync.Mutex
	root      *File
	Versioned bool
	Files     map[rpc.FID]*File
	IOunit    rpc.MaxSize
}

var (
	debug = flag.Int("debug", 0, "print debug messages")
	root  = flag.String("root", "/", "Set the root for all attaches")
)

func (e FileServer) Rversion(msize rpc.MaxSize, version string) (rpc.MaxSize, string, error) {
	if version != "9P2000" {
		return 0, "", fmt.Errorf("%v not supported; only 9P2000", version)
	}
	e.Versioned = true
	return msize, version, nil
}

func (e FileServer) getFile(fid rpc.FID) (*File, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	f, ok := e.Files[fid]
	if !ok {
		return nil, fmt.Errorf("Bad FID")
	}

	return f, nil
}

func (e FileServer) Rattach(fid rpc.FID, afid rpc.FID, aname string, _ string) (rpc.QID, error) {
	if afid != rpc.NOFID {
		return rpc.QID{}, fmt.Errorf("We don't do auth attach")
	}
	// There should be no .. or other such junk in the Aname. Clean it up anyway.
	fmt.Fprintf(os.Stderr, "-------------------------- %v ---------------", []byte(aname))
	aname = path.Join("/", aname)
	aname = path.Join(*root, aname)
	fmt.Fprintf(os.Stderr, "=================== stat %v =====================", aname)
	_, _ = os.Stat("FUCK")
	st, err := os.Stat(aname)
	fmt.Fprintf(os.Stderr, "=================== %v %v =====================", st, err)
	if err != nil {
		return rpc.QID{}, err
	}
	r := &File{fullName: aname}
	r.QID = dirToQID(st)
	e.Files[fid] = r
	e.root = r
	return r.QID, nil
}

func (e FileServer) Rflush(f rpc.FID, t rpc.FID) error {
	switch int(f) {
	case 2:
		// Make it fancier, later.
		return nil
	}
	return fmt.Errorf("Read: bad rpc.FID %v", f)
}

func (e FileServer) Rwalk(fid rpc.FID, newfid rpc.FID, paths []string) ([]rpc.QID, error) {
	e.mu.Lock()
	f, ok := e.Files[fid]
	e.mu.Unlock()
	if !ok {
		return nil, fmt.Errorf("Bad FID")
	}
	if len(paths) == 0 {
		e.mu.Lock()
		defer e.mu.Unlock()
		of, ok := e.Files[newfid]
		if ok {
			return nil, fmt.Errorf("FID in use")
		}
		e.Files[newfid] = of
		return []rpc.QID{of.QID}, nil
	}
	p := f.fullName
	q := make([]rpc.QID, len(paths))

	// optional: path.Join(p, paths[i]...) and see if the endpoint exists and return if not.
	// It can save a little time. Maybe later.
	for i := range paths {
		p = path.Join(p, paths[i])
		st, err := os.Lstat(p)
		if err != nil {
			return nil, fmt.Errorf("ENOENT")
		}
		q[i] = dirToQID(st)
	}
	st, err := os.Lstat(p)
	if err != nil {
		return nil, fmt.Errorf("Walk succeeded but stat failed")
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	// this is quite unlikely, which is why we don't bother checking for it first.
	if _, ok := e.Files[newfid]; ok {
		return nil, fmt.Errorf("FID in use")
	}
	e.Files[newfid] = &File{fullName: p, QID: dirToQID(st)}
	return q, nil
}

func (e FileServer) Ropen(fid rpc.FID, mode rpc.Mode) (rpc.QID, rpc.MaxSize, error) {
	e.mu.Lock()
	f, ok := e.Files[fid]
	e.mu.Unlock()
	if !ok {
		return rpc.QID{}, 0, fmt.Errorf("Bad FID")
	}

	var err error
	f.File, err = os.OpenFile(f.fullName, omode2uflags(mode), 0)
	if err != nil {
		return rpc.QID{}, 0, err
	}

	return f.QID, e.IOunit, nil
}
func (e FileServer) Rcreate(fid rpc.FID, name string, perm rpc.Perm, mode rpc.Mode) (rpc.QID, rpc.MaxSize, error) {
	//fmt.Printf("open(%v, %v\n", fid, mode)
	return rpc.QID{}, 5000, nil
}
func (e FileServer) Rclunk(fid rpc.FID) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if _, ok := e.Files[fid]; !ok {
		return fmt.Errorf("Bad FID")
	}
	delete(e.Files, fid)
	return nil
}

func (e FileServer) Rstat(fid rpc.FID) (rpc.Dir, error) {
	f, err := e.getFile(fid)
	if err != nil {
		return rpc.Dir{}, err
	}
	st, err := os.Lstat(f.fullName)
	if err != nil {
		return rpc.Dir{}, fmt.Errorf("ENOENT")
	}
	d, err := dirTo9p2000Dir(path.Base(f.fullName), st)
	if err != nil {
		return rpc.Dir{}, nil
	}
	return *d, nil
}
func (e FileServer) Rwstat(f rpc.FID, d rpc.Dir) error {
	return fmt.Errorf("Permission denied")
}
func (e FileServer) Rremove(f rpc.FID) error {
	return fmt.Errorf("Permission denied")
}

func (e FileServer) Rread(fid rpc.FID, o rpc.Offset, c rpc.Count) ([]byte, error) {
	f, err := e.getFile(fid)
	if err != nil {
		return nil, err
	}
	if f.File == nil {
		return nil, fmt.Errorf("FID not open")
	}
	if f.QID.Type&rpc.QTDIR != 0 {
		return nil, fmt.Errorf("Permission denied")
		/*
			if tc.Offset == 0 {
				var e error
				// If we got here, it was open. Can't really seek
				// in most cases, just close and reopen it.
				fid.file.Close()
				if fid.file, e = os.OpenFile(fid.path, omode2uflags(req.Fid.Omode), 0); e != nil {
					req.RespondError(toError(e))
					return
				}

				if fid.dirs, e = fid.file.Readdir(-1); e != nil {
					req.RespondError(toError(e))
					return
				}

				if dbg {
					log.Printf("Read: read %d entries", len(fid.dirs))
				}
				fid.dirents = nil
				fid.direntends = nil
				for i := 0; i < len(fid.dirs); i++ {
					path := fid.path + "/" + fid.dirs[i].Name()
					st, err := dir2Dir(path, fid.dirs[i], req.Conn.Dotu, req.Conn.Srv.Upool)
					if err != nil {
						if dbg {
							log.Printf("dbg: stat of %v: %v", path, err)
						}
						continue
					}
					if dbg {
						log.Printf("Stat: %v is %v", path, st)
					}
					b := ninep.PackDir(st, req.Conn.Dotu)
					fid.dirents = append(fid.dirents, b...)
					count += len(b)
					fid.direntends = append(fid.direntends, count)
					if dbg {
						log.Printf("fid.direntends is %v\n", fid.direntends)
					}
				}
			}

			switch {
			case tc.Offset > uint64(len(fid.dirents)):
				count = 0
			case len(fid.dirents[tc.Offset:]) > int(tc.Count):
				count = int(tc.Count)
			default:
				count = len(fid.dirents[tc.Offset:])
			}

			if dbg {
				log.Printf("readdir: count %v @ offset %v", count, tc.Offset)
			}
			nextend := sort.SearchInts(fid.direntends, int(tc.Offset)+count)
			if nextend < len(fid.direntends) {
				if fid.direntends[nextend] > int(tc.Offset)+count {
					if nextend > 0 {
						count = fid.direntends[nextend-1] - int(tc.Offset)
					} else {
						count = 0
					}
				}
			}
			if dbg {
				log.Printf("readdir: count adjusted %v @ offset %v", count, tc.Offset)
			}
			if count == 0 && int(tc.Offset) < len(fid.dirents) && len(fid.dirents) > 0 {
				req.RespondError(&ninep.Error{"too small read size for dir entry", ninep.EINVAL})
				return
			}
			copy(rc.Data, fid.dirents[tc.Offset:int(tc.Offset)+count])
		*/
	}
	// N.B. even if they ask for 0 bytes on some file systems it is important to pass
	// through a zero byte read (not Unix, of course).
	b := make([]byte, c)
	n, err := f.File.ReadAt(b, int64(o))
	if err != nil {
		return nil, err
	}
	return b[:n], nil
}

func (e FileServer) Rwrite(fid rpc.FID, o rpc.Offset, c rpc.Count, b []byte) (rpc.Count, error) {
	f, err := e.getFile(fid)
	if err != nil {
		return -1, err
	}
	if f.File == nil {
		return -1, fmt.Errorf("FID not open")
	}

	// N.B. even if they ask for 0 bytes on some file systems it is important to pass
	// through a zero byte write (not Unix, of course). Also, let the underlying file system
	// manage the error if the open mode was wrong. No need to duplicate the logic.

	n, err := f.File.WriteAt(b[:c], int64(o))
	return rpc.Count(n), err
}

type ServerOpt func(*rpc.Server) error

func NewUFS(opts ...rpc.ServerOpt) (*rpc.Server, error) {
	f := FileServer{}
	f.Files = make(map[rpc.FID]*File)
	f.mu = &sync.Mutex{}
	// any opts for the ufs layer can be added here too ...
	s, err := next.NewServer(f, opts...)
	if err != nil {
		return nil, err
	}
	f.IOunit = 8192
	s.Start()
	return s, nil
}