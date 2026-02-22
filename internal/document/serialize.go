package document

import (
	"encoding/binary"
	"encoding/json"
	"errors"
	"math"
	"time"
)

// Binary format flags.
const (
	flagTombstone  byte = 1 << 0
	flagHasContent byte = 1 << 1
	flagHasAttrs   byte = 1 << 2
	flagHasEmbed   byte = 1 << 3
)

// Serialize encodes a Document into a binary representation for SSTable storage.
// Format: flags(1) | version(8) | created_at(8) | updated_at(8)
//
//	| content_len(4) | content(N)        (if flagHasContent)
//	| attrs_len(4)   | attrs_json(N)     (if flagHasAttrs)
//	| embed_len(4)   | embed_f32s(N*4)   (if flagHasEmbed)
func Serialize(doc *Document) ([]byte, error) {
	var flags byte
	if doc.Content != "" {
		flags |= flagHasContent
	}
	if len(doc.Attributes) > 0 {
		flags |= flagHasAttrs
	}
	if len(doc.Embedding) > 0 {
		flags |= flagHasEmbed
	}

	// Pre-compute size.
	size := 1 + 8 + 8 + 8 // flags + version + timestamps
	if flags&flagHasContent != 0 {
		size += 4 + len(doc.Content)
	}

	var attrsJSON []byte
	if flags&flagHasAttrs != 0 {
		var err error
		attrsJSON, err = json.Marshal(doc.Attributes)
		if err != nil {
			return nil, err
		}
		size += 4 + len(attrsJSON)
	}
	if flags&flagHasEmbed != 0 {
		size += 4 + len(doc.Embedding)*4
	}

	buf := make([]byte, size)
	off := 0

	buf[off] = flags
	off++

	binary.LittleEndian.PutUint64(buf[off:], doc.Version)
	off += 8

	binary.LittleEndian.PutUint64(buf[off:], uint64(doc.CreatedAt.UnixNano()))
	off += 8

	binary.LittleEndian.PutUint64(buf[off:], uint64(doc.UpdatedAt.UnixNano()))
	off += 8

	if flags&flagHasContent != 0 {
		binary.LittleEndian.PutUint32(buf[off:], uint32(len(doc.Content)))
		off += 4
		off += copy(buf[off:], doc.Content)
	}

	if flags&flagHasAttrs != 0 {
		binary.LittleEndian.PutUint32(buf[off:], uint32(len(attrsJSON)))
		off += 4
		off += copy(buf[off:], attrsJSON)
	}

	if flags&flagHasEmbed != 0 {
		binary.LittleEndian.PutUint32(buf[off:], uint32(len(doc.Embedding)))
		off += 4
		for _, f := range doc.Embedding {
			binary.LittleEndian.PutUint32(buf[off:], math.Float32bits(f))
			off += 4
		}
	}

	return buf, nil
}

// SerializeTombstone returns a single-byte tombstone marker.
func SerializeTombstone() []byte {
	return []byte{flagTombstone}
}

// IsTombstone returns true if the value represents a deleted document.
func IsTombstone(data []byte) bool {
	return len(data) > 0 && data[0]&flagTombstone != 0
}

// Deserialize decodes a binary representation back into a Document.
// The caller must set the ID separately (it's the key, not stored in the value).
func Deserialize(data []byte) (*Document, error) {
	if len(data) < 1 {
		return nil, errors.New("empty data")
	}

	flags := data[0]
	if flags&flagTombstone != 0 {
		return nil, errors.New("cannot deserialize tombstone")
	}

	if len(data) < 25 { // 1 + 8 + 8 + 8
		return nil, errors.New("data too short")
	}

	off := 1
	doc := &Document{}

	doc.Version = binary.LittleEndian.Uint64(data[off:])
	off += 8

	doc.CreatedAt = time.Unix(0, int64(binary.LittleEndian.Uint64(data[off:]))).UTC()
	off += 8

	doc.UpdatedAt = time.Unix(0, int64(binary.LittleEndian.Uint64(data[off:]))).UTC()
	off += 8

	if flags&flagHasContent != 0 {
		if off+4 > len(data) {
			return nil, errors.New("truncated content length")
		}
		cLen := int(binary.LittleEndian.Uint32(data[off:]))
		off += 4
		if off+cLen > len(data) {
			return nil, errors.New("truncated content")
		}
		doc.Content = string(data[off : off+cLen])
		off += cLen
	}

	if flags&flagHasAttrs != 0 {
		if off+4 > len(data) {
			return nil, errors.New("truncated attrs length")
		}
		aLen := int(binary.LittleEndian.Uint32(data[off:]))
		off += 4
		if off+aLen > len(data) {
			return nil, errors.New("truncated attrs")
		}
		doc.Attributes = make(map[string]any)
		if err := json.Unmarshal(data[off:off+aLen], &doc.Attributes); err != nil {
			return nil, err
		}
		off += aLen
	}

	if flags&flagHasEmbed != 0 {
		if off+4 > len(data) {
			return nil, errors.New("truncated embedding length")
		}
		eLen := int(binary.LittleEndian.Uint32(data[off:]))
		off += 4
		if off+eLen*4 > len(data) {
			return nil, errors.New("truncated embedding")
		}
		doc.Embedding = make([]float32, eLen)
		for i := range doc.Embedding {
			doc.Embedding[i] = math.Float32frombits(binary.LittleEndian.Uint32(data[off:]))
			off += 4
		}
	}

	return doc, nil
}
