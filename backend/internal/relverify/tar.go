package relverify

import (
	"archive/tar"
	"bufio"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"hash"
	"io"
	"strings"
)

const (
	chunkSize = 32 << 10
	// maxEntries bounds the tar entries Inspect looks at: the directory and at
	// most eight files, with room for one more that is then rejected.
	maxEntries = 16
	// expansionSlack is what the decompressed stream may hold beyond the file
	// contents: headers, padding and the end-of-archive blocks.
	expansionSlack = 1 << 20
)

// Sink receives the files of a bundle that Inspect has read. Inspect never
// writes a file itself.
//
// The contract, which the helper's staging code (G6.5) implements and relies on:
//
//   - Create is called once per file, before the first Write of that file, with
//     the file's name below the top-level directory (for example "oarfish"; never
//     a path), the mode from the manifest (0644 or 0755) and the size from the
//     manifest. The implementation chooses where and how to write (O_EXCL, an
//     explicit mode, root ownership). Create is not called for a program that
//     fails the ELF check, and not at all when the sink is nil.
//   - Write receives the bytes in order. They are not verified yet: the hash of
//     a file is known only after its last byte.
//   - Finish is called once, after the last Write, and only when the file's size
//     and SHA-256 equal the manifest's. It is never called for a file that
//     failed, and never after any failure.
//
// Consequences the caller must handle. A file for which Create was called and
// Finish was not is partial and unverified. When Inspect fails, files that were
// already finished were each verified, but the release as a whole is not
// (a later entry can still fail): the caller must remove everything the sink
// wrote, that is, the whole staging directory, and use none of it. A caller that
// wants to write nothing for a bad release runs Inspect twice: first with a nil
// sink, which only verifies, then with the real sink; the second run verifies
// again, so a change between the two runs is caught as well.
type Sink interface {
	Create(name string, mode uint32, size int64) (File, error)
}

// File is one file being written to a Sink.
type File interface {
	Write(p []byte) (int, error)
	Finish() error
}

// limitError is an internal sentinel. It is a constant, not a variable, so the
// package has no mutable global state.
type limitError string

func (e limitError) Error() string { return string(e) }

const (
	errExpansion = limitError("decompressed size limit")
	errSize      = limitError("file size limit")
	errSink      = limitError("sink")
)

// Inspect streams the gzip-compressed tarball of a bundle, checks it against the
// manifest (which must come from ParseManifest) and hands every verified file to
// sink (nil: verify only). ADR 0010 U5, "extraction rules":
//
//   - The first entry is the single top-level directory oarfish_<version>_linux_arm64/.
//     After it only regular files are accepted. Every other entry type (symbolic
//     link, hard link, device, FIFO, a directory below the top level, a second
//     directory entry), an absolute path, a ".." element, a second top-level name,
//     a duplicate and every name outside the allow-list (layout) fails. Allowed
//     names are matched as exact strings, so no other spelling can pass.
//   - Every file is in contents with the same size, and its SHA-256 equals the
//     manifest's. The header size must equal the manifest size, and each file is
//     read through a limit of its manifest size plus one byte. The mode comes from
//     the manifest; the owner, group and mode of the tar header are ignored.
//   - The found files are exactly those of contents. Because contents is exactly
//     the file set of the shape the components object declares (ParseManifest
//     checks that, and Inspect checks it again), the four component files are in
//     the bundle if and only if the manifest has a components object.
//   - oarfish and oarfish-update-helper start with an ELF64 little-endian header
//     for machine 0xB7.
//   - The stream is one gzip member with nothing after it. The decompressed size
//     is bounded by the sum of the manifest sizes plus 1 MiB, so a decompression
//     bomb stops at that limit, whatever it hides in padding or in a header. Bytes
//     after the end-of-archive marker must be zero.
//
// Memory is bounded: a 32 KiB buffer for the copy, one for the header peek, and
// the decompressor's window; the bundle is never held. Inspect starts no goroutine.
//
// Failures are *Error: CodeChecksumMismatch for a size or hash that differs from
// the manifest, CodeRequirementsNotMet for a program that is not an arm64 ELF
// executable, CodeIOError for an error of the bundle reader or of the sink, and
// CodeManifestInvalid for every other violation (ADR 0010 U2 has no code for a
// bad bundle; this is the nearest one).
func Inspect(bundle io.Reader, m *Manifest, sink Sink) error {
	if bundle == nil || m == nil {
		return fail(CodeInternal, "Inspect needs a bundle reader and a manifest")
	}
	ver, err := m.Semver()
	if err != nil || m.Bundle.File != BundleFileName(ver) {
		return fail(CodeInternal, "the manifest was not produced by ParseManifest")
	}
	components := m.Components != nil
	root := bundleRoot(m.Version)
	want := make(map[string]Entry, len(m.Contents))
	var total int64
	for _, e := range m.Contents {
		// A Manifest is a plain struct, so Inspect re-checks what it relies on:
		// each entry is a distinct allow-listed file of the declared shape with
		// its mode, and the sizes stay within the limits.
		name := strings.TrimPrefix(e.Path, root+"/")
		lf, ok := lookupLayout(name, components)
		if !ok || name == e.Path || want[e.Path].Path != "" || e.Mode != lf.mode || e.Size < 1 || e.Size > maxContentBytes {
			return fail(CodeInternal, "the manifest was not produced by ParseManifest")
		}
		want[e.Path] = e
		total += e.Size
	}
	if len(m.Contents) != len(layout(components)) || total > maxContentBytes {
		return fail(CodeInternal, "the manifest was not produced by ParseManifest")
	}

	src := &errReader{r: bundle}
	br := bufio.NewReaderSize(src, chunkSize)
	zr, err := gzip.NewReader(br)
	if err != nil {
		if e := ioErr(src); e != nil {
			return e
		}
		return fail(CodeManifestInvalid, "the bundle is not a gzip stream")
	}
	zr.Multistream(false)
	lr := &limitReader{r: zr, n: total + expansionSlack}
	tr := tar.NewReader(lr)

	in := &inspector{
		tr: tr, sink: sink, buf: bufio.NewReaderSize(nil, chunkSize), src: src, lr: lr,
		root: root, want: want, components: components, seen: make(map[string]bool, len(want)),
	}
	if err := in.run(); err != nil {
		return err
	}
	// Past the end-of-archive marker: only zero padding, then the end of the
	// gzip member (which checks its CRC and length), then the end of the input.
	var pad [512]byte
	for {
		k, err := lr.Read(pad[:])
		for _, b := range pad[:k] {
			if b != 0 {
				return fail(CodeManifestInvalid, "the bundle has data after the end of the archive")
			}
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return in.streamErr(err)
		}
	}
	if _, err := br.Peek(1); err != io.EOF {
		if err != nil {
			if e := ioErr(src); e != nil {
				return e
			}
			return fail(CodeManifestInvalid, "the bundle could not be read to its end")
		}
		return fail(CodeManifestInvalid, "the bundle has data after the gzip stream")
	}
	return nil
}

type inspector struct {
	tr         *tar.Reader
	sink       Sink
	buf        *bufio.Reader
	src        *errReader
	lr         *limitReader
	root       string
	want       map[string]Entry
	components bool
	seen       map[string]bool
}

func (in *inspector) run() error {
	sawRoot := false
	found := 0
	var total int64
	for i := 0; ; i++ {
		if i >= maxEntries {
			return fail(CodeManifestInvalid, "the bundle has too many entries")
		}
		hdr, err := in.tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return in.streamErr(err)
		}
		if !sawRoot {
			if hdr.Typeflag != tar.TypeDir || hdr.Name != in.root+"/" {
				return fail(CodeManifestInvalid, "the first entry is not the top-level directory")
			}
			sawRoot = true
			continue
		}
		if hdr.Typeflag != tar.TypeReg {
			return entryTypeError(hdr.Typeflag)
		}
		e, ok := in.want[hdr.Name]
		if !ok {
			return pathError(hdr.Name, in.root)
		}
		if in.seen[hdr.Name] {
			return fail(CodeManifestInvalid, "the bundle has an entry twice")
		}
		in.seen[hdr.Name] = true
		name := strings.TrimPrefix(hdr.Name, in.root+"/")
		lf, _ := lookupLayout(name, in.components)
		if hdr.Size != e.Size {
			return fail(CodeChecksumMismatch, "an entry size differs from the manifest")
		}
		total += e.Size
		if total > maxContentBytes {
			return fail(CodeManifestInvalid, "the files add up to more than 128 MiB")
		}
		if err := in.file(name, e, lf.elf); err != nil {
			return err
		}
		found++
	}
	if !sawRoot {
		return fail(CodeManifestInvalid, "the bundle has no top-level directory")
	}
	if found != len(in.want) {
		return fail(CodeManifestInvalid, "a file of the manifest is missing from the bundle")
	}
	return nil
}

// file reads one file through a limit of its manifest size plus one byte,
// hashes it, checks the ELF header of a program before the sink sees any byte,
// and calls Finish only when size and hash match.
func (in *inspector) file(name string, e Entry, elf bool) error {
	in.buf.Reset(io.LimitReader(in.tr, e.Size+1))
	if elf {
		head, err := in.buf.Peek(20)
		if err != nil && err != io.EOF && err != bufio.ErrBufferFull {
			return in.streamErr(err)
		}
		if !isELFArm64(head) {
			return fail(CodeRequirementsNotMet, "a program in the bundle is not a 64-bit little-endian ELF executable for aarch64")
		}
	}
	var w File
	if in.sink != nil {
		var err error
		if w, err = in.sink.Create(name, e.Mode, e.Size); err != nil {
			return fail(CodeIOError, "the sink could not create a file")
		}
	}
	dst := &fileWriter{w: w, h: sha256.New(), limit: e.Size}
	if _, err := in.buf.WriteTo(dst); err != nil {
		switch {
		case errors.Is(err, errSize):
			return fail(CodeChecksumMismatch, "a file is larger than its manifest size")
		case dst.sinkErr:
			return fail(CodeIOError, "the sink could not write a file")
		}
		return in.streamErr(err)
	}
	if dst.n != e.Size {
		return fail(CodeChecksumMismatch, "a file is smaller than its manifest size")
	}
	if hex.EncodeToString(dst.h.Sum(nil)) != e.SHA256 {
		return fail(CodeChecksumMismatch, "a file's SHA-256 differs from the manifest")
	}
	if w != nil {
		if err := w.Finish(); err != nil {
			return fail(CodeIOError, "the sink could not finish a file")
		}
	}
	return nil
}

// streamErr maps an error of the decompressor or the tar reader.
func (in *inspector) streamErr(err error) error {
	if errors.Is(err, errExpansion) {
		return fail(CodeManifestInvalid, "the bundle expands beyond the size limit")
	}
	if e := ioErr(in.src); e != nil {
		return e
	}
	return fail(CodeManifestInvalid, "the bundle is not a complete, well-formed tar.gz")
}

// ioErr is CodeIOError when the bundle reader itself failed, and nil when the
// bytes it delivered are what is bad.
func ioErr(src *errReader) error {
	if src.err != nil {
		return fail(CodeIOError, "the bundle could not be read")
	}
	return nil
}

// entryTypeError is the failure for a tar entry that is not a regular file.
func entryTypeError(t byte) error {
	switch t {
	case tar.TypeSymlink:
		return fail(CodeManifestInvalid, "the bundle contains a symbolic link")
	case tar.TypeLink:
		return fail(CodeManifestInvalid, "the bundle contains a hard link")
	case tar.TypeChar, tar.TypeBlock:
		return fail(CodeManifestInvalid, "the bundle contains a device")
	case tar.TypeFifo:
		return fail(CodeManifestInvalid, "the bundle contains a FIFO")
	case tar.TypeDir:
		return fail(CodeManifestInvalid, "the bundle contains a directory below the top level")
	}
	return fail(CodeManifestInvalid, "the bundle contains an entry that is not a regular file")
}

// pathError is the failure for a regular file whose name is not in contents. It
// says why without repeating the name.
func pathError(name, root string) error {
	switch {
	case strings.HasPrefix(name, "/"):
		return fail(CodeManifestInvalid, "the bundle contains an absolute path")
	case hasDotDot(name):
		return fail(CodeManifestInvalid, "the bundle contains a .. path element")
	case !strings.HasPrefix(name, root+"/"):
		return fail(CodeManifestInvalid, "the bundle contains a second top-level name")
	}
	return fail(CodeManifestInvalid, "the bundle contains a file that is not in the allow-list or not in the manifest")
}

func hasDotDot(name string) bool {
	for _, el := range strings.Split(name, "/") {
		if el == ".." {
			return true
		}
	}
	return false
}

// errReader remembers the first non-EOF error of the bundle reader, to tell a
// failing disk from a bad archive.
type errReader struct {
	r   io.Reader
	err error
}

func (e *errReader) Read(p []byte) (int, error) {
	n, err := e.r.Read(p)
	if err != nil && err != io.EOF && e.err == nil {
		e.err = err
	}
	return n, err
}

// limitReader returns at most n bytes of r. When r has more, it fails with
// errExpansion instead of ending the stream quietly.
type limitReader struct {
	r io.Reader
	n int64
}

func (l *limitReader) Read(p []byte) (int, error) {
	if l.n <= 0 {
		var b [1]byte
		k, err := l.r.Read(b[:])
		if k > 0 {
			return 0, errExpansion
		}
		return 0, err
	}
	if int64(len(p)) > l.n {
		p = p[:l.n]
	}
	k, err := l.r.Read(p)
	l.n -= int64(k)
	return k, err
}

// fileWriter hashes, counts and forwards the bytes of one file, and refuses the
// byte after the last one the manifest allows.
type fileWriter struct {
	w       File
	h       hash.Hash
	n       int64
	limit   int64
	sinkErr bool
}

func (f *fileWriter) Write(p []byte) (int, error) {
	if f.n+int64(len(p)) > f.limit {
		return 0, errSize
	}
	f.n += int64(len(p))
	f.h.Write(p)
	if f.w != nil {
		if n, err := f.w.Write(p); err != nil || n != len(p) {
			f.sinkErr = true
			return n, errSink
		}
	}
	return len(p), nil
}
