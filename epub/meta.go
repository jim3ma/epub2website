package epub

import (
	"path"
	"os"
	"github.com/PuerkitoBio/goquery"
	"strings"
	"encoding/xml"
	"bytes"
	"fmt"
	"io/ioutil"
	"text/template"
	"time"
	"path/filepath"
)

type MetaInfo struct {
	RootFile RootFile `xml:"rootfiles>rootfile"`
}

type RootFile struct {
	Path string `xml:"full-path,attr"`
}

type OPF struct {
	//Manifests []Manifest `xml:"manifest>item"`
	Guides []Guide `xml:"guide>reference"`
}

type Manifest struct {
	Href string `xml:"href,attr"`
	Id   string `xml:"id,attr"`
}

type Guide struct {
	Title string `xml:"title,attr"`
	Type  string `xml:"type,attr"`
	Href  string `xml:"href,attr"`
}

type NCX struct {
	NavMap     []*NavPoint `xml:"navMap>navPoint"`
	Guides     []Guide     `xml:"guide>reference"`
	Navigation string
	WorkDir    string
	OutDir     string
	GitbookUrl string
}

type NavPoint struct {
	Title        string      `xml:"navLabel>text"`
	SubNavPoints []*NavPoint `xml:"navPoint"`
	Content      content     `xml:"content"`

	NCX      *NCX
	Depth    int
	Level    string
	HtmlPath string // x/a.html
	Src      string // a.html#1
	SrcRaw   string // a.html
	Dir      string // x
	/*
	WorkDir    string
	OutDir     string
	GitbookUrl string
	*/

	Next *NavPoint
	Prev *NavPoint

	Navigation string

	// read from html
	HeadLinks string
	Body      string
}

type content struct {
	Src string `xml:"src,attr"`
}

func NewNcx(ncxPath string, outDir string, gitbook string, guides []Guide) (*NCX, error) {
	tocFile, _ := os.OpenFile(ncxPath, os.O_RDONLY, 0644)
	defer tocFile.Close()
	tocData, _ := ioutil.ReadAll(tocFile)
	ncx := &NCX{}
	err := xml.Unmarshal(tocData, ncx)
	if err != nil {
		return nil, err
	}
	ncx.WorkDir = path.Dir(ncxPath)
	ncx.OutDir = outDir
	ncx.GitbookUrl = gitbook

	for _, g := range guides {
		if g.Type == "cover" {
			break
		}
		href := g.Href
		ext := path.Ext(href)
		if ext == ".xhtml" {
			href = href[0:len(href)-len(ext)] + ".html"
			os.Rename(path.Join(ncx.WorkDir, g.Href), path.Join(ncx.WorkDir, href))
		}
		// TODO fix mismatch url in cover when cover is not in the directory which content in.
		ncx.NavMap = append([]*NavPoint{&NavPoint{
			Title: g.Title,
			Content: content{
				Src: href,
			},
		}}, ncx.NavMap...)
	}

	for _, g := range guides {
		if g.Type != "cover" {
			continue
		}
		href := g.Href
		ext := path.Ext(href)
		if ext == ".xhtml" {
			href = href[0:len(href)-len(ext)] + ".html"
			os.Rename(path.Join(ncx.WorkDir, g.Href), path.Join(ncx.WorkDir, href))
		}
		// TODO fix mismatch url in cover when cover is not in the directory which content in.
		ncx.NavMap = append([]*NavPoint{&NavPoint{
			Title: g.Title,
			Content: content{
				Src: href,
			},
		}}, ncx.NavMap...)
	}

	ncx.UpdateNavMap()
	return ncx, nil
}

func (ncx *NCX) Render() (first *NavPoint, err error) {
	navPoint := ncx.NavMap[len(ncx.NavMap)-1]
	first = ncx.NavMap[0]
	// 逆序打印，这样每一页的标题会是第一个指向该页面的标题
	for navPoint != nil {
		navi, err := ncx.RenderNavigation(navPoint)
		if err != nil {
			return nil, err
		}
		_, err = navPoint.RenderPage(navi)
		if err != nil {
			return nil, err
		}
		navPoint = navPoint.Prev
	}
	return first, nil
}

func (ncx *NCX) RenderNavigation(np *NavPoint) (string, error) {
	var buf bytes.Buffer
	naviFile, err := os.OpenFile("./template/navigation.html", os.O_RDONLY, 0644)
	if err != nil {
		return "", err
	}
	defer naviFile.Close()
	navi, err := ioutil.ReadAll(naviFile)
	if err != nil {
		return "", err
	}
	tmpl, err := template.New("navi").Funcs(
		template.FuncMap{
			"rel": np.RelativePath,
		},
	).Parse(string(navi))
	if err != nil {
		return "", err
	}
	err = tmpl.Execute(&buf, ncx.NavMap)
	if err != nil {
		return "", err
	}
	ncx.Navigation = buf.String()
	return ncx.Navigation, nil
}

func (ncx *NCX) UpdateNavMap() {
	var prev *NavPoint
	for i, v := range ncx.NavMap {
		prev = ncx.updateNavPoint(prev, v, 0, i, "")
	}
}

func (ncx *NCX) updateNavPoint(prev *NavPoint, nav *NavPoint, depth int, index int, level string) (last *NavPoint) {
	nav.Prev = prev
	nav.Depth = depth + 1
	sharpIdx := strings.Index(nav.Content.Src, "#")
	if sharpIdx == -1 {
		nav.HtmlPath = nav.Content.Src
	} else {
		nav.HtmlPath = nav.Content.Src[0:sharpIdx]
	}
	nav.SrcRaw = path.Base(nav.Content.Src)
	nav.Src = path.Base(nav.HtmlPath)
	nav.Dir = path.Dir(nav.Content.Src)

	nav.NCX = ncx
	/*
	nav.WorkDir = ncx.WorkDir
	nav.GitbookUrl = ncx.GitbookUrl
	nav.OutDir = ncx.OutDir
	*/

	if prev != nil {
		prev.Next = nav
	}
	if level != "" {
		nav.Level = fmt.Sprintf("%s.%d", level, index+1)
	} else {
		nav.Level = fmt.Sprintf("%d", index+1)
	}
	//fmt.Printf("%#v\n", nav)
	last = nav
	for i, v := range nav.SubNavPoints {
		last = ncx.updateNavPoint(last, v, depth+1, i, nav.Level)
	}
	if last == nil {
		last = nav
	}
	return
}

func (np *NavPoint) RenderPage(navi string) (string, error) {
	var buf bytes.Buffer

	pageFile, err := os.OpenFile("./template/page.html", os.O_RDONLY, 0644)
	if err != nil {
		return "", err
	}
	defer pageFile.Close()
	page, err := ioutil.ReadAll(pageFile)
	if err != nil {
		return "", err
	}
	tmpl, err := template.New("page").Funcs(
		template.FuncMap{
			"next": np.FindNextHtml,
			"prev": np.FindPrevHtml,
			"rel":  np.RelativePath,
			"now":  now,
		},
	).Parse(string(page))
	if err != nil {
		return "", err
	}
	np.Navigation = navi
	err = np.loadHtml()
	if err != nil {
		return "", err
	}
	err = tmpl.Execute(&buf, np)
	if err != nil {
		return "", err
	}
	// TODO highlight navigation
	ret := buf.String()
	np.save(ret)
	return ret, nil
}

func (np *NavPoint) save(data string) error {
	outPath := path.Join(np.NCX.OutDir, np.HtmlPath)
	//fmt.Println(outPath)
	outDir := path.Dir(outPath)
	if _, err := os.Stat(outDir); os.IsNotExist(err) {
		os.MkdirAll(outDir, os.ModePerm)
	}
	return ioutil.WriteFile(outPath, []byte(data), 0644)
}

func (np *NavPoint) loadHtml() error {
	htmlPath := path.Join(np.NCX.WorkDir, np.HtmlPath)
	htmlFile, err := os.OpenFile(htmlPath, os.O_RDONLY, 0644)
	if err != nil {
		return err
	}
	defer htmlFile.Close()
	doc, err := goquery.NewDocumentFromReader(htmlFile)
	if err != nil {
		return err
	}
	np.Body, err = doc.Find("body").First().Html()
	if err != nil {
		return err
	}
	np.HeadLinks, err = doc.Find("head").First().Html()
	if err != nil {
		return err
	}
	// TODO just find link in head tag
	heads := strings.Split(np.HeadLinks, "\n")
	np.HeadLinks = ""
	for _, v := range heads {
		if strings.Contains(v, "link") {
			np.HeadLinks += v
		}
	}
	return nil
}

func (np *NavPoint) FindNextHtml() *NavPoint {
	p := np.Next
	for p != nil {
		if p.Src != np.Src {
			return p
		}
		p = p.Next
	}
	return nil
}

func (np *NavPoint) FindPrevHtml() *NavPoint {
	p := np.Prev
	for p != nil {
		if p.Src != np.Src {
			return p
		}
		p = p.Prev
	}
	return nil
}

func (np *NavPoint) RelativePath(npx *NavPoint) string {
	rel, _ := filepath.Rel(np.Dir, npx.Content.Src)
	//fmt.Printf("np.Dir: %s, npx.Content.Src: %s, rel: %s\n", np.Dir, npx.Content.Src, rel)
	return rel
}

func now() string {
	t := time.Now()
	return t.Format("2006-01-02T15:04:05Z07:00")
}
