package filelist

import (
	"compress/bzip2"
	"encoding/xml"
	"io"

	"github.com/dennwc/go-dcpp/adc/types"
	"github.com/dennwc/go-dcpp/tiger"
)

type Dir struct {
	Name       string `xml:"Name,attr"`
	Incomplete int    `xml:"Incomplete,attr"`
	Dirs       []Dir  `xml:"Directory"`
	Files      []File `xml:"File"`
}

type File struct {
	Name string     `xml:"Name,attr"`
	Size int64      `xml:"Size,attr"`
	TTH  tiger.Hash `xml:"TTH,attr"`
}

type FileList struct {
	Version   int       `xml:"Version,attr"`
	CID       types.CID `xml:"CID,attr"`
	Base      string    `xml:"Base,attr"`
	Generator string    `xml:"Generator,attr"`
	Dirs      []Dir     `xml:"Directory"`
	Files     []File    `xml:"File"`
}

func Decode(r io.Reader) (*FileList, error) {
	var list FileList
	err := xml.NewDecoder(r).Decode(&list)
	if err != nil {
		return nil, err
	}
	return &list, nil
}

func DecodeBZIP(r io.Reader) (*FileList, error) {
	return Decode(bzip2.NewReader(r))
}
