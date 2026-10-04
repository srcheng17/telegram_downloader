package cbzedit

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"slices"
	"strconv"
	"strings"

	"github.com/ryancheng/telegram-downloader/internal/comicinfo"
)

const (
	localSignature   = 0x04034b50
	centralSignature = 0x02014b50
	eocdSignature    = 0x06054b50
	dataSignature    = 0x08074b50
)

type entryFingerprint struct {
	name       string
	local      [32]byte
	central    [32]byte
	raw        [32]byte
	content    [32]byte
	compressed uint64
	expanded   uint64
}

type archiveState struct {
	Inspection
	entries  []entryFingerprint
	comment  string
	xmlIndex int
}

func normalizeLimits(l Limits) Limits {
	if l.MaxArchiveBytes <= 0 {
		l.MaxArchiveBytes = 1 << 30
	}
	if l.MaxEntries <= 0 {
		l.MaxEntries = 10000
	}
	if l.MaxEntryBytes <= 0 {
		l.MaxEntryBytes = 128 << 20
	}
	if l.MaxTotalExpandedBytes <= 0 {
		l.MaxTotalExpandedBytes = 2 << 30
	}
	if l.MaxXMLBytes <= 0 || l.MaxXMLBytes > comicinfo.MaxXMLBytes {
		l.MaxXMLBytes = comicinfo.MaxXMLBytes
	}
	return l
}

// openBook confines all source operations to a checked parent directory. A
// caller still serializes edits to the same book; external writers are caught
// by whole-file SHA checks rather than a false cross-process CAS claim.
func openBook(root *os.Root, relative string) (*os.Root, string, *os.File, os.FileInfo, error) {
	if root == nil {
		return nil, "", nil, nil, ErrInvalidArchive
	}
	parts, err := relativeParts(relative)
	if err != nil || !strings.EqualFold(path.Ext(relative), ".cbz") {
		return nil, "", nil, nil, ErrInvalidArchive
	}
	parent, err := checkedParent(root, parts[:len(parts)-1])
	if err != nil {
		return nil, "", nil, nil, err
	}
	base := parts[len(parts)-1]
	info, err := parent.Lstat(base)
	if err != nil || !info.Mode().IsRegular() {
		parent.Close()
		return nil, "", nil, nil, ErrInvalidArchive
	}
	file, err := parent.Open(base)
	if err != nil {
		parent.Close()
		return nil, "", nil, nil, err
	}
	opened, err := file.Stat()
	if err != nil || !opened.Mode().IsRegular() || !os.SameFile(info, opened) {
		file.Close()
		parent.Close()
		return nil, "", nil, nil, ErrConflict
	}
	return parent, base, file, info, nil
}

func relativeParts(name string) ([]string, error) {
	if name == "" || strings.HasPrefix(name, "/") || strings.ContainsAny(name, "\\\x00") {
		return nil, ErrInvalidArchive
	}
	parts := strings.Split(name, "/")
	for _, part := range parts {
		if part == "" || part == "." || part == ".." {
			return nil, ErrInvalidArchive
		}
	}
	return parts, nil
}

func checkedParent(root *os.Root, parts []string) (*os.Root, error) {
	current, err := root.OpenRoot(".")
	if err != nil {
		return nil, err
	}
	for _, part := range parts {
		info, err := current.Lstat(part)
		if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			current.Close()
			return nil, ErrInvalidArchive
		}
		next, err := current.OpenRoot(part)
		current.Close()
		if err != nil {
			return nil, err
		}
		current = next
	}
	return current, nil
}

func inspectRoot(ctx context.Context, root *os.Root, relative string, limits Limits) (archiveState, error) {
	parent, _, file, info, err := openBook(root, relative)
	if err != nil {
		return archiveState{}, err
	}
	defer parent.Close()
	defer file.Close()
	state, err := inspectFile(ctx, file, info.Size(), limits)
	if err != nil {
		return archiveState{}, err
	}
	after, err := file.Stat()
	if err != nil || !os.SameFile(info, after) || after.Size() != info.Size() || !after.ModTime().Equal(info.ModTime()) {
		return archiveState{}, ErrConflict
	}
	return state, nil
}

func inspectFile(ctx context.Context, file *os.File, size int64, limits Limits) (archiveState, error) {
	state := archiveState{xmlIndex: -1}
	if err := ctx.Err(); err != nil {
		return state, err
	}
	if size <= 0 || size > limits.MaxArchiveBytes {
		return state, ErrLimit
	}
	reader, err := zip.NewReader(file, size)
	if err != nil {
		return state, fmt.Errorf("open CBZ: %w", ErrInvalidArchive)
	}
	if len(reader.File) == 0 || len(reader.File) > limits.MaxEntries {
		return state, ErrLimit
	}
	central, err := scanStructure(file, size, reader.File)
	if err != nil {
		return state, err
	}
	state.comment = reader.Comment
	if looksLikeMetadataComment(reader.Comment) {
		return state, ErrInvalidArchive
	}
	state.entries = make([]entryFingerprint, 0, len(reader.File))
	var total uint64
	for i, member := range reader.File {
		if err := ctx.Err(); err != nil {
			return state, err
		}
		if !safeMemberName(member.Name) || member.Flags&^uint16(0x808) != 0 || member.Method != zip.Store && member.Method != zip.Deflate {
			return state, ErrInvalidArchive
		}
		if member.UncompressedSize64 > uint64(limits.MaxEntryBytes) || member.CompressedSize64 > uint64(limits.MaxArchiveBytes) {
			return state, ErrLimit
		}
		total += member.UncompressedSize64
		if total > uint64(limits.MaxTotalExpandedBytes) {
			return state, ErrLimit
		}
		nameBase := path.Base(member.Name)
		if strings.EqualFold(nameBase, "ComicInfo.xml") {
			if member.Name != "ComicInfo.xml" || state.xmlIndex >= 0 || member.UncompressedSize64 > uint64(limits.MaxXMLBytes) {
				return state, ErrInvalidArchive
			}
			state.xmlIndex = i
			state.HasComicInfo = true
		}
		if competingMetadata(nameBase) {
			return state, ErrInvalidArchive
		}
		if imageMember(member.Name) {
			state.PageCount++
		}
		fingerprint := entryFingerprint{name: member.Name, local: central[i].local, central: central[i].central, compressed: member.CompressedSize64, expanded: member.UncompressedSize64}
		raw, err := member.OpenRaw()
		if err != nil {
			return state, ErrInvalidArchive
		}
		fingerprint.raw, err = hashBounded(ctx, raw, int64(member.CompressedSize64))
		if err != nil {
			return state, err
		}
		stream, err := member.Open()
		if err != nil {
			return state, ErrInvalidArchive
		}
		var xmlData bytes.Buffer
		// Keep hashing separate from the optional XML capture; the ZIP reader
		// verifies the member CRC when its stream reaches EOF.
		contentHasher := sha256.New()
		var sink io.Writer = contentHasher
		if i == state.xmlIndex {
			sink = io.MultiWriter(contentHasher, &xmlData)
		}
		read, copyErr := io.Copy(sink, io.LimitReader(contextReader{ctx, stream}, limits.MaxEntryBytes+1))
		closeErr := stream.Close()
		if ctx.Err() != nil {
			return state, ctx.Err()
		}
		if copyErr != nil || closeErr != nil || read != int64(member.UncompressedSize64) {
			return state, ErrInvalidArchive
		}
		copy(fingerprint.content[:], contentHasher.Sum(nil))
		if i == state.xmlIndex {
			state.ComicInfo = xmlData.Bytes()
		}
		state.entries = append(state.entries, fingerprint)
	}
	if state.HasComicInfo {
		if _, err := parseSafeXML(state.ComicInfo, state.PageCount); err != nil {
			return state, err
		}
	}
	state.EntryCount = len(reader.File)
	state.Size = size
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return state, err
	}
	whole, err := hashReader(ctx, io.LimitReader(file, size+1))
	if err != nil {
		return state, err
	}
	state.SHA256 = hex.EncodeToString(whole[:])
	return state, nil
}

type headerFingerprint struct{ local, central [32]byte }

// scanStructure rejects prefixes, trailers, ZIP64, local-only extras, holes,
// reordered local records and unsupported descriptors. Those structures can be
// read by archive/zip but cannot be shown to survive zip.Writer.Copy unchanged.
func scanStructure(file *os.File, size int64, files []*zip.File) ([]headerFingerprint, error) {
	tailLen := min(size, 65557)
	tail, err := readAt(file, size-tailLen, int(tailLen))
	if err != nil {
		return nil, ErrInvalidArchive
	}
	eocd := int64(-1)
	for i := len(tail) - 22; i >= 0; i-- {
		if binary.LittleEndian.Uint32(tail[i:]) == eocdSignature && i+22+int(binary.LittleEndian.Uint16(tail[i+20:])) == len(tail) {
			eocd = size - tailLen + int64(i)
			break
		}
	}
	if eocd < 0 {
		return nil, ErrInvalidArchive
	}
	e, err := readAt(file, eocd, 22)
	if err != nil || binary.LittleEndian.Uint16(e[4:]) != 0 || binary.LittleEndian.Uint16(e[6:]) != 0 || binary.LittleEndian.Uint16(e[8:]) != uint16(len(files)) || binary.LittleEndian.Uint16(e[10:]) != uint16(len(files)) || len(files) >= 65535 {
		return nil, ErrInvalidArchive
	}
	centralSize := int64(binary.LittleEndian.Uint32(e[12:]))
	centralStart := int64(binary.LittleEndian.Uint32(e[16:]))
	if centralStart < 0 || centralSize < 0 || centralStart+centralSize != eocd {
		return nil, ErrInvalidArchive
	}
	position := centralStart
	localPosition := int64(0)
	result := make([]headerFingerprint, 0, len(files))
	for _, member := range files {
		h, err := readAt(file, position, 46)
		if err != nil || binary.LittleEndian.Uint32(h) != centralSignature {
			return nil, ErrInvalidArchive
		}
		nameLen, extraLen, commentLen := int(binary.LittleEndian.Uint16(h[28:])), int(binary.LittleEndian.Uint16(h[30:])), int(binary.LittleEndian.Uint16(h[32:]))
		entryLen := 46 + nameLen + extraLen + commentLen
		full, err := readAt(file, position, entryLen)
		if err != nil || position+int64(entryLen) > eocd || binary.LittleEndian.Uint16(h[34:]) != 0 || binary.LittleEndian.Uint16(h[36:]) != 0 || binary.LittleEndian.Uint32(h[42:]) != uint32(localPosition) {
			return nil, ErrInvalidArchive
		}
		if binary.LittleEndian.Uint32(h[20:]) == ^uint32(0) || binary.LittleEndian.Uint32(h[24:]) == ^uint32(0) || binary.LittleEndian.Uint16(h[6:]) > 20 {
			return nil, ErrInvalidArchive
		}
		local, err := readAt(file, localPosition, 30)
		if err != nil || binary.LittleEndian.Uint32(local) != localSignature {
			return nil, ErrInvalidArchive
		}
		localNameLen, localExtraLen := int(binary.LittleEndian.Uint16(local[26:])), int(binary.LittleEndian.Uint16(local[28:]))
		localFull, err := readAt(file, localPosition, 30+localNameLen+localExtraLen)
		if err != nil || localNameLen != nameLen || localExtraLen != extraLen || !bytes.Equal(localFull[30:], full[46:46+nameLen+extraLen]) || !bytes.Equal(local[4:6], h[6:8]) || !bytes.Equal(local[6:14], h[8:16]) {
			return nil, ErrInvalidArchive
		}
		if string(full[46:46+nameLen]) != member.Name || uint64(binary.LittleEndian.Uint32(h[20:])) != member.CompressedSize64 || uint64(binary.LittleEndian.Uint32(h[24:])) != member.UncompressedSize64 {
			return nil, ErrInvalidArchive
		}
		dataOffset, err := member.DataOffset()
		if err != nil || dataOffset != localPosition+int64(len(localFull)) {
			return nil, ErrInvalidArchive
		}
		next := dataOffset + int64(member.CompressedSize64)
		if next > centralStart || next < dataOffset {
			return nil, ErrInvalidArchive
		}
		if member.Flags&0x8 == 0 {
			if !bytes.Equal(local[14:26], h[16:28]) {
				return nil, ErrInvalidArchive
			}
		} else {
			if !zeroOrEqual(local[14:26], h[16:28]) {
				return nil, ErrInvalidArchive
			}
			descriptor, err := readAt(file, next, 16)
			if err != nil || binary.LittleEndian.Uint32(descriptor) != dataSignature || !bytes.Equal(descriptor[4:], h[16:28]) {
				return nil, ErrInvalidArchive
			}
			next += 16
		}
		centralCopy := slices.Clone(full)
		clear(centralCopy[42:46])
		result = append(result, headerFingerprint{local: sha256.Sum256(localFull), central: sha256.Sum256(centralCopy)})
		localPosition = next
		position += int64(entryLen)
	}
	if position != eocd || localPosition != centralStart {
		return nil, ErrInvalidArchive
	}
	return result, nil
}

func zeroOrEqual(local, central []byte) bool {
	return bytes.Equal(local, central) || bytes.Equal(local, make([]byte, len(local)))
}

func readAt(file *os.File, offset int64, count int) ([]byte, error) {
	if offset < 0 || count < 0 {
		return nil, ErrInvalidArchive
	}
	data := make([]byte, count)
	_, err := file.ReadAt(data, offset)
	return data, err
}

func safeMemberName(name string) bool {
	if name == "" || strings.HasPrefix(name, "/") || strings.ContainsAny(name, "\\\x00") {
		return false
	}
	trimmed := strings.TrimSuffix(name, "/")
	parts := strings.Split(trimmed, "/")
	for _, part := range parts {
		if part == "" || part == "." || part == ".." {
			return false
		}
	}
	return true
}

func imageMember(name string) bool {
	switch strings.ToLower(path.Ext(name)) {
	case ".jpg", ".jpeg", ".png", ".webp", ".gif", ".bmp", ".avif":
		return true
	default:
		return false
	}
}

func competingMetadata(name string) bool {
	switch strings.ToLower(name) {
	case "metroninfo.xml", "comicbookinfo.json", "comicinfo.json", "comet.xml", "metadata.json":
		return true
	default:
		return false
	}
}

func looksLikeMetadataComment(comment string) bool {
	trimmed := strings.TrimSpace(comment)
	return strings.HasPrefix(trimmed, "{") || strings.HasPrefix(trimmed, "[") || strings.HasPrefix(trimmed, "<")
}

type parsedXML struct {
	values   map[string]string
	pages    []comicinfo.Page
	hasPages bool
}

func parseSafeXML(data []byte, imageCount int) (parsedXML, error) {
	if len(data) == 0 || len(data) > comicinfo.MaxXMLBytes {
		return parsedXML{}, ErrInvalidXML
	}
	parsed, err := comicinfo.Parse(data)
	if err != nil || len(parsed.Warnings) > 0 {
		return parsedXML{}, ErrInvalidXML
	}
	decoder := xml.NewDecoder(bytes.NewReader(bytes.TrimPrefix(data, []byte{0xef, 0xbb, 0xbf})))
	depth := 0
	hasPages := false
	for {
		token, err := decoder.Token()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return parsedXML{}, ErrInvalidXML
		}
		switch element := token.(type) {
		case xml.StartElement:
			depth++
			if depth == 1 {
				for _, attr := range element.Attr {
					if !standardNamespaceDeclaration(attr) {
						return parsedXML{}, ErrInvalidXML
					}
				}
			}
			if depth == 2 && element.Name.Local == "Pages" {
				hasPages = true
			}
		case xml.EndElement:
			depth--
		}
	}
	seen := map[int]bool{}
	for _, page := range parsed.Pages {
		for _, attr := range page.Attributes {
			if attr.Name.Local != "Image" {
				continue
			}
			index, err := strconv.Atoi(attr.Value)
			if err != nil || index < 0 || index >= imageCount || seen[index] {
				return parsedXML{}, ErrInvalidXML
			}
			seen[index] = true
		}
	}
	return parsedXML{values: parsed.Values, pages: parsed.Pages, hasPages: hasPages}, nil
}

// The canonical ComicInfo serializer drops namespace declarations. These two
// standard, unused declarations are safe to omit; every other root attribute
// still blocks a rewrite because its meaning cannot be preserved.
func standardNamespaceDeclaration(attr xml.Attr) bool {
	if attr.Name.Space != "xmlns" {
		return false
	}
	switch attr.Name.Local {
	case "xsi":
		return attr.Value == "http://www.w3.org/2001/XMLSchema-instance"
	case "xsd":
		return attr.Value == "http://www.w3.org/2001/XMLSchema"
	default:
		return false
	}
}

type contextReader struct {
	ctx context.Context
	r   io.Reader
}

func (r contextReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.r.Read(p)
}

func hashReader(ctx context.Context, r io.Reader) ([32]byte, error) {
	h := sha256.New()
	_, err := io.Copy(h, contextReader{ctx, r})
	var sum [32]byte
	copy(sum[:], h.Sum(nil))
	return sum, err
}

func hashBounded(ctx context.Context, r io.Reader, size int64) ([32]byte, error) {
	h := sha256.New()
	n, err := io.Copy(h, io.LimitReader(contextReader{ctx, r}, size+1))
	if ctx.Err() != nil {
		return [32]byte{}, ctx.Err()
	}
	if err != nil || n != size {
		return [32]byte{}, ErrInvalidArchive
	}
	var sum [32]byte
	copy(sum[:], h.Sum(nil))
	return sum, nil
}
