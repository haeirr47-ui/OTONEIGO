package mediacache

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"os"
)

const integrityChunk = int64(1024 * 1024)
const maxManifest = 128 * 1024 * 1024

func validChunks(e *entry) bool {
	if len(e.Chunks) != int((e.Bytes+integrityChunk-1)/integrityChunk) || e.Bytes <= 0 || e.Bytes > maxAudio {
		return false
	}
	for _, h := range e.Chunks {
		if !validKey(h) {
			return false
		}
	}
	return true
}

// chunkReader copies only from the exact buffer whose digest was checked. A
// later disk mutation cannot change already verified bytes between hash/read.
// One reader retains at most one MiB and never caches an unverified chunk.
type chunkReader struct {
	f      *os.File
	ctx    context.Context
	size   int64
	hashes []string
	pos    int64
	chunk  int64
	data   []byte
	failed func()
	abort  bool
}

func newChunkReader(ctx context.Context, f *os.File, e *entry, failed func()) *chunkReader {
	return &chunkReader{f: f, ctx: ctx, size: e.Bytes, hashes: e.Chunks, chunk: -1, failed: failed}
}
func (r *chunkReader) fail() error {
	r.data = nil
	r.chunk = -1
	if r.ctx.Err() == nil && r.failed != nil {
		r.failed()
	}
	return errCache
}
func (r *chunkReader) load(i int64) error {
	if err := r.ctx.Err(); err != nil {
		return err
	}
	if i == r.chunk {
		return nil
	}
	if i < 0 || i >= int64(len(r.hashes)) {
		return r.fail()
	}
	st, err := r.f.Stat()
	if err != nil || st.Size() != r.size {
		return r.fail()
	}
	size := integrityChunk
	if size > r.size-i*integrityChunk {
		size = r.size - i*integrityChunk
	}
	p := make([]byte, int(size))
	n, err := r.f.ReadAt(p, i*integrityChunk)
	if n != len(p) || err != nil && err != io.EOF {
		return r.fail()
	}
	h := sha256.Sum256(p)
	if hex.EncodeToString(h[:]) != r.hashes[i] {
		return r.fail()
	}
	if err := r.ctx.Err(); err != nil {
		return err
	}
	r.data = p
	r.chunk = i
	return nil
}
func (r *chunkReader) ReadAt(p []byte, off int64) (int, error) {
	if off < 0 {
		return 0, errCache
	}
	if len(p) == 0 {
		return 0, nil
	}
	if off >= r.size {
		return 0, io.EOF
	}
	n := 0
	for n < len(p) && off < r.size {
		if err := r.load(off / integrityChunk); err != nil {
			return n, err
		}
		copied := copy(p[n:], r.data[off%integrityChunk:])
		off += int64(copied)
		n += copied
	}
	if n < len(p) {
		return n, io.EOF
	}
	return n, nil
}
func (r *chunkReader) Read(p []byte) (int, error) {
	n, err := r.ReadAt(p, r.pos)
	r.pos += int64(n)
	if r.abort && err != nil && err != io.EOF {
		panic(http.ErrAbortHandler)
	}
	return n, err
}
func (r *chunkReader) Seek(off int64, whence int) (int64, error) {
	switch whence {
	case io.SeekStart:
	case io.SeekCurrent:
		off += r.pos
	case io.SeekEnd:
		off += r.size
	default:
		return 0, errCache
	}
	if off < 0 {
		return 0, errCache
	}
	r.pos = off
	return off, nil
}

// chunkDigests derives a bounded index in the same pass as the complete hash.
// Feed accepts arbitrary transport read boundaries.
type chunkDigests struct {
	pending []byte
	hashes  []string
}

func (c *chunkDigests) Write(p []byte) {
	for len(p) > 0 {
		take := int(integrityChunk) - len(c.pending)
		if take > len(p) {
			take = len(p)
		}
		c.pending = append(c.pending, p[:take]...)
		p = p[take:]
		if int64(len(c.pending)) == integrityChunk {
			c.flush()
		}
	}
}
func (c *chunkDigests) flush() {
	if len(c.pending) > 0 {
		h := sha256.Sum256(c.pending)
		c.hashes = append(c.hashes, hex.EncodeToString(h[:]))
		c.pending = c.pending[:0]
	}
}
func (c *chunkDigests) finish() []string { c.flush(); return c.hashes }
