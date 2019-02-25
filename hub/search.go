package hub

import (
	"context"

	"github.com/direct-connect/go-dcpp/tiger"
)

type TTH = tiger.Hash

type FileType int

const (
	FileTypeAny    = FileType(0)
	FileTypeFolder = FileType(1 << iota)
	FileTypePicture
	FileTypeAudio
	FileTypeVideo
	FileTypeCompressed
	FileTypeDocuments
	FileTypeExecutable
)

type SearchReq struct {
	Pattern  string // TODO: define what it means
	MinSize  uint64
	MaxSize  uint64
	FileType FileType
}

func (r *SearchReq) Validate(res SearchResult) bool {
	if r == nil {
		return true
	}
	if r.FileType == FileTypeFolder {
		if _, ok := res.(Dir); !ok {
			return false
		}
	} else if r.FileType != FileTypeAny {
		if _, ok := res.(File); !ok {
			return false
		}
	}
	if r.MinSize != 0 && r.MaxSize != 0 {
		if f, ok := res.(File); ok {
			if r.MinSize != 0 && f.Size < r.MinSize {
				return false
			}
			if r.MaxSize != 0 && f.Size > r.MaxSize {
				return false
			}
		}
	}
	// TODO: validate pattern
	return true
}

type SearchResult interface {
	From() Peer
	isSearchItem()
}

type File struct {
	Peer Peer
	Path string
	Size uint64
	TTH  *TTH
}

func (f File) isSearchItem() {}
func (f File) From() Peer {
	return f.Peer
}

type Dir struct {
	Peer Peer
	Path string
}

func (f Dir) isSearchItem() {}
func (f Dir) From() Peer {
	return f.Peer
}

type Search interface {
	Peer() Peer
	SendResult(r SearchResult) error
	Close() error
}

func (h *Hub) Search(req SearchReq, s Search) {
	peer := s.Peer()
	// FIXME: should be bound to the close channel of the peer
	ctx := context.TODO()
	for _, p := range h.Peers() {
		if p == peer {
			continue
		} else if p.User().Share == 0 {
			continue
		}
		_ = p.Search(ctx, req, s)
	}
}

func (h *Hub) SearchTTH(tth TTH, s Search) {
	peer := s.Peer()
	// FIXME: should be bound to the close channel of the peer
	ctx := context.TODO()
	for _, p := range h.Peers() {
		if p == peer {
			continue
		} else if p.User().Share == 0 {
			continue
		}
		_ = p.SearchTTH(ctx, tth, s)
	}
}
