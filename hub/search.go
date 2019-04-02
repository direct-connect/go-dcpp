package hub

import (
	"context"

	"github.com/direct-connect/go-dc/tiger"
)

type TTH = tiger.Hash

type FileType int

const (
	FileTypeAny     = FileType(0)
	FileTypePicture = FileType(1 << iota)
	FileTypeAudio
	FileTypeVideo
	FileTypeCompressed
	FileTypeDocuments
	FileTypeExecutable
)

type NameSearch struct {
	And []string
	Not []string
}

func (NameSearch) isSearchReq() {}
func (f NameSearch) MatchName(name string) bool {
	return true // TODO
}
func (f NameSearch) Match(r SearchResult) bool {
	switch r := r.(type) {
	case Dir:
		return f.MatchName(r.Path)
	case File:
		return f.MatchName(r.Path)
	}
	return true
}

type TTHSearch TTH

func (TTHSearch) isSearchReq() {}
func (h TTHSearch) Match(r SearchResult) bool {
	switch r := r.(type) {
	case File:
		if r.TTH != nil {
			return TTH(h) == *r.TTH
		}
	}
	return true
}

type SearchRequest interface {
	Match(r SearchResult) bool
	isSearchReq()
}

type FileSearch struct {
	NameSearch
	Ext      []string
	NoExt    []string
	FileType FileType
	MinSize  uint64
	MaxSize  uint64
}

func (FileSearch) isSearchReq() {}
func (s FileSearch) Match(r SearchResult) bool {
	switch r := r.(type) {
	case File:
		if s.MinSize != 0 && s.MaxSize != 0 {
			if s.MinSize != 0 && r.Size < s.MinSize {
				return false
			}
			if s.MaxSize != 0 && r.Size > s.MaxSize {
				return false
			}
		}
		return s.NameSearch.MatchName(r.Path)
	}
	return false
}

type DirSearch struct {
	NameSearch
}

func (DirSearch) isSearchReq() {}
func (s DirSearch) Match(r SearchResult) bool {
	switch r := r.(type) {
	case Dir:
		return s.NameSearch.MatchName(r.Path)
	}
	return false
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

func (h *Hub) Search(req SearchRequest, s Search, peers []Peer) {
	cntSearch.Add(1)
	defer measure(durSearch)()

	peer := s.Peer()
	if peers == nil {
		peers = h.Peers()
	}
	// FIXME: should be bound to the close channel of the peer
	ctx := context.TODO()
	for _, p := range peers {
		if p == peer {
			continue
		} else if p.User().Share == 0 {
			continue
		}
		_ = p.Search(ctx, req, s)
	}
}
