package epub

import (
	"encoding/xml"
	"io/ioutil"
	"os"
)

type NavDoc struct {
	XMLName xml.Name `xml:"html"`
	Body    Body     `xml:"body"`
}

type Body struct {
	XMLName xml.Name `xml:"body"`
	Nav     []*Nav   `xml:"nav"`
}

type Nav struct {
	XMLName xml.Name `xml:"nav"`
	Item    *Item    `xml:"ol"`
	Type    string   `xml:"type,attr"`
}

type Item struct {
	ItemInner []*ItemInner `xml:"li"`
}

type ItemInner struct {
	SubItem *Item  `xml:"ol"`
	Anchor  anchor `xml:"a"`
}

type anchor struct {
	Title string `xml:",innerxml"`
	Href  string `xml:"href,attr"`
}

func LoadNavDoc(navPath string) *NavDoc {
	f, err := os.Open(navPath)
	if err != nil {
		panic(err)
	}
	data, err := ioutil.ReadAll(f)
	if err != nil {
		panic(err)
	}
	doc := &NavDoc{}
	err = xml.Unmarshal(data, &doc)
	if err != nil {
		panic(err)
	}
	return doc
}
