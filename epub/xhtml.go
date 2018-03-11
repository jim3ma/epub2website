package epub

import "encoding/xml"

type xhtml struct {
	XMLName xml.Name `xml:"html"`
	Title   string   `xml:"head>title"`
	Body    XBody    `xml:"body"`
}

type XBody struct {
	Inner string `xml:",innerxml"`
}
