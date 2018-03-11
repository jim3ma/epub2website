package epub

import (
	"path"
	"os"
	"strings"
	"encoding/xml"
	"bytes"
	"fmt"
	"io/ioutil"
	"text/template"
	"time"
	"path/filepath"
	"net/http"

	"github.com/PuerkitoBio/goquery"
	"github.com/rakyll/statik/fs"
	_ "github.com/jim3ma/epub2website/statik"
)

const (
	MediaTypeCSS       = "text/css"
	MediaTypeImageJPEG = "image/jpeg"
	MediaTypeImagePNG  = "image/png"
	MediaTypeImageGIF  = "image/gif"
	MediaTypeHTML      = "application/xhtml+xml"
	MediaTypeNCX       = "application/x-dtbncx+xml"
)

var templatefs http.FileSystem

type MetaInfo struct {
	RootFile RootFile `xml:"rootfiles>rootfile"`
}

type RootFile struct {
	Path string `xml:"full-path,attr"`
}

type OPF struct {
	Manifests []*ManifestItem `xml:"manifest>item"`
	Spine     []*ItemRef      `xml:"spine>itemref"`
	Guides    []Guide         `xml:"guide>reference"`
	Dir       string
}

func (opf *OPF) findNavDoc() *ManifestItem {
	for _, item := range opf.Manifests {
		if item.Properties == "nav" {
			return item
		}
	}
	return nil
}

type ItemRef struct {
	Idref string `xml:"idref,attr"`
}

type ManifestItem struct {
	Href       string `xml:"href,attr"`
	Id         string `xml:"id,attr"`
	MediaType  string `xml:"media-type,attr"`
	Properties string `xml:"properties,attr"`
}

type Guide struct {
	Title string `xml:"title,attr"`
	Type  string `xml:"type,attr"`
	Href  string `xml:"href,attr"`
}

type NCX struct {
	NavMap     []*NavPoint     `xml:"navMap>navPoint"`
	Guides     []Guide         `xml:"-"`
	Styles     []*ManifestItem `xml:"-"`
	Navigation string          `xml:"-"`
	WorkDir    string          `xml:"-"`
	OutDir     string          `xml:"-"`
	GitbookUrl string          `xml:"-"`
}

type NavPoint struct {
	Title        string      `xml:"navLabel>text"`
	SubNavPoints []*NavPoint `xml:"navPoint"`
	Content      content     `xml:"content"`

	NCX      *NCX   `xml:"-"`
	Depth    int    `xml:"-"`
	Level    string `xml:"-"`
	HtmlPath string `xml:"-"` // x/a.html
	Src      string `xml:"-"` // a.html#1
	SrcRaw   string `xml:"-"` // a.html
	Dir      string `xml:"-"` // x

	Next *NavPoint `xml:"-"`
	Prev *NavPoint `xml:"-"`

	Navigation string `xml:"-"`

	// read from html
	HeadLinks string `xml:"-"`
	Body      string `xml:"-"`
}

type content struct {
	Src string `xml:"src,attr"`
}

func NewNcx(ncxPath string, outDir string, gitbook string, opf *OPF) (*NCX, error) {
	ncx := &NCX{}
	ncx.WorkDir = path.Dir(ncxPath)

	// generate a ncx file from opf spine section
	if _, err := os.Stat(ncxPath); os.IsNotExist(err) {
		// search TOC in OPF first
		if nav := opf.findNavDoc(); nav != nil {
			navDoc := LoadNavDoc(path.Join(ncx.WorkDir, nav.Href))
			ncx.GenerateFromNavDoc(navDoc, path.Dir(nav.Href))
		} else {
			// Finally, we have no choose, read spine from OPF
			ncx.GenerateFromSpine(opf)
		}
	} else {
		tocFile, _ := os.OpenFile(ncxPath, os.O_RDONLY, 0644)
		defer tocFile.Close()
		tocData, _ := ioutil.ReadAll(tocFile)
		err := xml.Unmarshal(tocData, ncx)
		if err != nil {
			return nil, err
		}
	}

	for _, m := range opf.Manifests {
		if m.MediaType == MediaTypeCSS {
			ncx.Styles = append(ncx.Styles, m)
		}
	}

	ncx.OutDir = outDir
	ncx.GitbookUrl = gitbook

	for _, g := range opf.Guides {
		if g.Type == "cover" {
			break
		}
		href := g.Href
		// TODO fix mismatch url in cover when cover is not in the directory which content in.
		ncx.NavMap = append([]*NavPoint{&NavPoint{
			Title: g.Title,
			Content: content{
				Src: href,
			},
		}}, ncx.NavMap...)
	}

	for _, g := range opf.Guides {
		if g.Type != "cover" {
			continue
		}
		href := g.Href
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

func (ncx *NCX) GenerateFromNavDoc(navDoc *NavDoc, relPath string) {
	var nav *Nav
	for _, v := range navDoc.Body.Nav {
		if v.Type == "toc" {
			nav = v
		}
	}
	if nav == nil {
		panic("error nav doc")
	}
	for _, item := range nav.Item.ItemInner {
		np := buildNavPointFromNavItem(item, relPath)
		ncx.NavMap = append(ncx.NavMap, np)
	}
}

func buildNavPointFromNavItem(item *ItemInner, relPath string)(np *NavPoint) {
	np = &NavPoint{
		Title: item.Anchor.Title,
		Content: content{
			Src: path.Join(relPath, item.Anchor.Href),
		},
	}
	if item.SubItem == nil {
		return
	}
	for _, sub := range item.SubItem.ItemInner{
		subnp := buildNavPointFromNavItem(sub, relPath)
		np.SubNavPoints = append(np.SubNavPoints, subnp)
	}
	return
}

func (ncx *NCX) GenerateFromSpine(opf *OPF) {
	for _, item := range opf.Spine {
		mf := opf.findManifestItem(item.Idref)
		htmlPath := path.Join(ncx.WorkDir, mf.Href)
		np := &NavPoint{
			Title: findTitle(htmlPath),
			Content: content{
				Src: mf.Href,
			},
		}
		ncx.NavMap = append(ncx.NavMap, np)
	}
}

func findTitle(htmlPath string) (title string) {
	htmlFile, err := os.OpenFile(htmlPath, os.O_RDONLY, 0644)
	if err != nil {
		panic(err)
	}
	defer htmlFile.Close()
	ext := path.Ext(htmlPath)
	switch ext {
	case ".xhtml":
		data, err := ioutil.ReadAll(htmlFile)
		if err != nil {
			panic(err)
		}
		x := xhtml{}
		err = xml.Unmarshal(data, &x)
		if err != nil {
			panic(err)
		}
		title = x.Title
	case ".html":
		doc, err := goquery.NewDocumentFromReader(htmlFile)
		if err != nil {
			panic(err)
		}
		title = doc.Find("title").Text()
		if len(title) > 128 {
			title = path.Base(htmlPath)
		}
	default:
		panic(fmt.Sprintf("unsupported file type: %s", htmlPath))
	}
	return
}

func (opf *OPF) findManifestItem(id string) *ManifestItem {
	for _, item := range opf.Manifests {
		if item.Id == id {
			return item
		}
	}
	return nil
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

func (ncx *NCX) BuildIndex() (err error) {
	navPoint := ncx.NavMap[0]
	for navPoint != nil {
		navPoint = navPoint.Next
	}
	return nil
}

func (ncx *NCX) RenderNavigation(np *NavPoint) (string, error) {
	var buf bytes.Buffer
	//naviFile, err := os.OpenFile("./template/navigation.html", os.O_RDONLY, 0644)
	naviFile, err := templatefs.Open("/navigation.html")
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
			"ext": np.UpdateExt,
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

	// pageFile, err := os.OpenFile("./template/page.html", os.O_RDONLY, 0644)
	pageFile, err := templatefs.Open("/page.html")
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
			"ext":  np.UpdateExt,
			"trim": np.TrimHash,
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
	// TODO just workaround for load all styles
	// should load styles from new page
	for _, style := range np.NCX.Styles {
		rel, _ := filepath.Rel(np.Dir, style.Href)
		np.HeadLinks = fmt.Sprintf(`%s<link href="%s" rel="stylesheet" type="text/css">`, np.HeadLinks, rel)
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
	if path.Ext(outPath) == ".xhtml" {
		idx := strings.LastIndex(outPath, ".")
		os.Remove(outPath)
		outPath = fmt.Sprintf("%s.html", outPath[:idx])
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
	ext := path.Ext(htmlPath)
	switch ext {
	case ".xhtml":
		data, err := ioutil.ReadAll(htmlFile)
		if err != nil {
			panic(err)
		}
		x := xhtml{}
		err = xml.Unmarshal(data, &x)
		if err != nil {
			panic(err)
		}
		np.Body = x.Body.Inner
	case ".html":
		doc, err := goquery.NewDocumentFromReader(htmlFile)
		if err != nil {
			return err
		}
		np.Body, err = doc.Find("body").First().Html()
		if err != nil {
			return err
		}
	default:
		panic(fmt.Sprintf("unsupported file type: %s", htmlPath))
	}
	/*
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
	*/
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

func (np *NavPoint) UpdateExt(orig string) string {
	if strings.HasPrefix(path.Ext(orig),".xhtml") {
		idx := strings.LastIndex(orig, ".")
		orig = strings.Replace(orig,".xhtml", ".html", idx)
	}
	return orig
}

func (np *NavPoint) TrimHash(orig string) string {
	if strings.Contains(orig, "html#"){
		idx := strings.LastIndex(orig, "#")
		orig = orig[:idx]
	}
	return orig
}

func now() string {
	t := time.Now()
	return t.Format("2006-01-02T15:04:05Z07:00")
}

func init() {
	var err error
	templatefs, err = fs.New()
	if err != nil {
		panic(err)
	}
}
