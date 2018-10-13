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
	"encoding/json"
	"net/url"
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
	Src      string `xml:"-"` // a.html
	SrcRaw   string `xml:"-"` // a.html#1
	Dir      string `xml:"-"` // x

	Next *NavPoint `xml:"-"`
	Prev *NavPoint `xml:"-"`

	Navigation string `xml:"-"`

	// read from html
	HeadLinks string `xml:"-"`
	Body      string `xml:"-"`

	from string
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
		d := xml.NewDecoder(bytes.NewReader(tocData))
		d.Strict = false
		err = d.Decode(&ncx)
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

	var cover *NavPoint
	for _, g := range opf.Guides {
		href := g.Href
		title := g.Title
		if title == "" {
			ext := path.Ext(g.Href)
			title = strings.Replace(path.Base(g.Href), ext, "", 0)
		}
		if findSubNav(ncx.NavMap, href) {
			continue
		}
		nav := &NavPoint{
			Title: title,
			Content: content{
				Src: href,
			},
			from: "guide",
		}
		// TODO fix mismatch url in cover when cover is not in the directory which content in.
		if g.Type == "cover" {
			cover = nav
			continue
		}
		ncx.NavMap = append([]*NavPoint{nav}, ncx.NavMap...)
	}

	if cover != nil {
		ncx.NavMap = append([]*NavPoint{cover}, ncx.NavMap...)
	}

	// merge spine into NcxMap for avoiding missing some pages
	// TODO find right Title in the missing pages
	ncx.MergeSpine(opf)

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

func buildNavPointFromNavItem(item *ItemInner, relPath string) (np *NavPoint) {
	np = &NavPoint{
		Title: item.Anchor.Title,
		Content: content{
			Src: path.Join(relPath, item.Anchor.Href),
		},
	}
	if item.SubItem == nil {
		return
	}
	for _, sub := range item.SubItem.ItemInner {
		subnp := buildNavPointFromNavItem(sub, relPath)
		np.SubNavPoints = append(np.SubNavPoints, subnp)
	}
	return
}

func (ncx *NCX) GenerateFromSpine(opf *OPF) {
	for _, item := range opf.Spine {
		mf := opf.findManifestItem(item.Idref)
		if mf == nil {
			continue
		}
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

// build a clean map with trim # after html
func buildCacheMap(npMap map[string]*NavPoint, nps []*NavPoint) {
	for _, nav := range nps {
		key, err := url.QueryUnescape(trimSharp(nav.Content.Src))
		if err != nil {
			key = trimSharp(nav.Content.Src)
		}
		//fmt.Println(key)
		npMap[key] = nav
		if len(nav.SubNavPoints) > 0 {
			buildCacheMap(npMap, nav.SubNavPoints)
		}
	}
}

func trimSharp(s string) string {
	sharpIdx := strings.Index(s, "#")
	if sharpIdx == -1 {
		return s
	} else {
		return s[0:sharpIdx]
	}
}

func findSubNav(sub []*NavPoint, src string) bool {
	for _, nav := range sub {
		if trimSharp(nav.Content.Src) == trimSharp(src) {
			return true
		}
		if len(nav.SubNavPoints) > 0 {
			return findSubNav(nav.SubNavPoints, src)
		}
	}
	return false
}

func (ncx *NCX) MergeSpine(opf *OPF) {
	// cacheMap is used for check whether the page is processed
	cacheMap := make(map[string]*NavPoint)
	// rawMap is used for quick check whether the spine page is in ncx map
	rawMap := make(map[string]*NavPoint)

	buildCacheMap(cacheMap, ncx.NavMap)
	buildCacheMap(rawMap, ncx.NavMap)
	findPrevNav := func(idx int) *NavPoint {
		for i := idx -1 ; i >= 0; i -- {
			mf := opf.findManifestItem(opf.Spine[i].Idref)
			if mf == nil {
				continue
			}
			trimPath, err := url.QueryUnescape(trimSharp(mf.Href))
			if err != nil {
				trimPath = mf.Href
			}
			if nav, ok := rawMap[trimPath]; ok {
				/*
				if nav.SubNavPoints != nil {
					// find last sub nav
					return FindLastSubNav(nav)
				}
				*/
				return nav
			}
		}
		return nil
	}
	for idx, item := range opf.Spine {
		mf := opf.findManifestItem(item.Idref)
		if mf == nil {
			continue
		}
		htmlPath := path.Join(ncx.WorkDir, mf.Href)
		// check src in NcxMap
		trimPath, err := url.QueryUnescape(trimSharp(mf.Href))
		if err != nil {
			trimPath = trimSharp(mf.Href)
		}
		old, ok := cacheMap[trimPath]
		// not in cacheMap
		if old == nil && !ok {
			//fmt.Printf("jim debug: page %s not found\n", mf.Href)
			nav := &NavPoint{
				// Spine page may not contain right title, try to find from H1, H2, H3 tag
				Title: findHTitle(htmlPath),
				Content: content{
					Src: mf.Href,
				},
			}
			if idx == 0 {
				cacheMap[trimSharp(nav.Content.Src)] = nav
				ncx.NavMap = append([]*NavPoint{nav}, ncx.NavMap...)
			} else {
				// find the first pre page which in rawMap and in cacheMap
				pre := findPrevNav(idx)
				if pre != nil {
					cacheMap[trimSharp(nav.Content.Src)] = nav
					pre.SubNavPoints = append(pre.SubNavPoints, nav)
				} else {
					panic("may be a bug, please report the trace to majinjing3@gmail.com")
					//fmt.Printf("jim debug: prev page %s not found\n", mf2.Href)
				}
			}
		}
	}
}

func findHTitle(htmlPath string) (title string) {
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
		d := xml.NewDecoder(bytes.NewReader(data))
		d.Strict = false
		err = d.Decode(&x)
		if err != nil {
			panic(err)
		}
		title = x.Title
	case ".html":
		doc, err := goquery.NewDocumentFromReader(htmlFile)
		if err != nil {
			panic(err)
		}
		title = doc.Find("h1").Text()
		if len(title) > 128 {
			title = path.Base(htmlPath)
		}
		if title == "" {
			title = doc.Find("h2").Text()
		}
		if title == "" {
			title = doc.Find("h3").Text()
		}
	default:
		panic(fmt.Sprintf("unsupported file type: %s", htmlPath))
	}
	return
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
		d := xml.NewDecoder(bytes.NewReader(data))
		d.Strict = false
		err = d.Decode(&x)
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
	navPoint := FindLastSubNav(ncx.NavMap[len(ncx.NavMap)-1])
	first = ncx.NavMap[0]
	navi, err := ncx.RenderNavigation(navPoint)
	// 逆序打印，这样每一页的标题会是第一个指向该页面的标题
	for navPoint != nil {
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

type DocIndex struct {
	Url      string `json:"url"`
	Title    string `json:"title"`
	Keywords string `json:"keywords"`
	Body     string `json:"body"`
}

func (ncx *NCX) BuildIndex() (err error) {
	navPoint := ncx.NavMap[0]
	indexs := make(map[string]*DocIndex)
	indexed := make(map[string]string)
	for ; navPoint != nil; navPoint = navPoint.Next {
		if _, ok := indexed[navPoint.Src]; ok {
			continue
		}
		indexed[navPoint.Src] = ""
		buf := bytes.NewBufferString(navPoint.Body)
		// free here
		navPoint.Body = ""
		doc, err := goquery.NewDocumentFromReader(buf)
		if err != nil {
			panic(err)
		}
		url := navPoint.UpdateExt(navPoint.Src)
		indexs[url] = &DocIndex{
			Url:      url,
			Title:    navPoint.Title,
			Keywords: "",
			Body:     doc.Text(),
		}
	}
	data, _ := json.Marshal(indexs)
	ioutil.WriteFile(path.Join(ncx.OutDir, "search_plus_index.json"), data, 0644)
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
			"rel":  np.RelativePath,
			"ext":  np.UpdateExt,
			"base": np.BasePath,
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

func (np *NavPoint) RenderPage(navi string) ([]byte, error) {
	var buf bytes.Buffer

	// pageFile, err := os.OpenFile("./template/page.html", os.O_RDONLY, 0644)
	pageFile, err := templatefs.Open("/page.html")
	if err != nil {
		return nil, err
	}
	defer pageFile.Close()
	page, err := ioutil.ReadAll(pageFile)
	if err != nil {
		return nil, err
	}
	tmpl, err := template.New("page").Funcs(
		template.FuncMap{
			"next": np.FindNextHtml,
			"prev": np.FindPrevHtml,
			"rel":  np.RelativePath,
			"base": np.BasePath,
			"ext":  np.UpdateExt,
			"trim": np.TrimHash,
			"now":  now,
		},
	).Parse(string(page))
	if err != nil {
		return nil, err
	}
	np.Navigation = navi
	err = np.loadHtml()
	if err != nil {
		return nil, err
	}
	// TODO just workaround for load all styles
	// should load styles from new page
	for _, style := range np.NCX.Styles {
		np.HeadLinks = fmt.Sprintf(`%s<link href="%s" rel="stylesheet" type="text/css">`, np.HeadLinks, style.Href)
	}
	err = tmpl.Execute(&buf, np)
	if err != nil {
		return nil, err
	}
	// TODO highlight navigation
	ret := buf.Bytes()
	np.save(ret)
	// should not free memory here
	// np.Body = ""
	return ret, nil
}

func (np *NavPoint) save(data []byte) error {
	outPath := path.Join(np.NCX.OutDir, np.Src)
	//fmt.Println(outPath)
	outDir := path.Dir(outPath)
	if _, err := os.Stat(outDir); os.IsNotExist(err) {
		os.MkdirAll(outDir, os.ModePerm)
	}
	if path.Ext(outPath) == ".xhtml" {
		idx := strings.LastIndex(outPath, ".")
		os.Remove(path.Join(np.NCX.OutDir, np.HtmlPath))
		outPath = fmt.Sprintf("%s.html", outPath[:idx])
	}
	return ioutil.WriteFile(outPath, data, 0644)
}

func (np *NavPoint) loadHtml() error {
	htmlPath := path.Join(np.NCX.WorkDir, np.HtmlPath)
	//fmt.Println(htmlPath)
	_, err := os.Stat(htmlPath)
	// url escape
	if os.IsNotExist(err) {
		htmlPath, err = url.QueryUnescape(htmlPath)
		if err != nil {
			return err
		}
		np.HtmlPath, err = url.QueryUnescape(np.HtmlPath)
		if err != nil {
			return err
		}
		np.Src, err = url.QueryUnescape(np.Src)
		if err != nil {
			return err
		}
	}
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
		d := xml.NewDecoder(bytes.NewReader([]byte(data)))
		d.Strict = false
		err = d.Decode(&x)
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
	case ".jpg":
		rel, _ := filepath.Rel(np.Dir, np.Content.Src)
		np.Body = fmt.Sprintf(`<img src="%s"/>`, rel)
		np.Src = strings.Replace(np.Src, ".jpg", ".html", strings.Index(np.Src, ".jpg"))
	default:
		panic(fmt.Sprintf("unsupported file type: %s", htmlPath))
	}
	buf := bytes.NewBufferString(np.Body)
	doc, err := goquery.NewDocumentFromReader(buf)
	// TODO update image src
	doc.Find("img").Each(func(i int, s *goquery.Selection) {
		src, exist := s.Attr("src")
		if !exist {
			return
		}
		newSrc := path.Join(np.Dir, path.Join(src))
		s.SetAttr("src", newSrc)
	})
	doc.Find("image").Each(func(i int, s *goquery.Selection) {
		src, exist := s.Attr("href")
		if !exist {
			return
		}
		newSrc := path.Join(np.Dir, path.Join(src))
		s.SetAttr("href", newSrc)
	})
	// update link href
	// 1. path
	// 2. ext
	doc.Find("a").Each(func(i int, s *goquery.Selection) {
		href, exist := s.Attr("href")
		if !exist {
			return
		}
		if path.IsAbs(href) ||
			strings.HasPrefix(href, "http://") ||
			strings.HasPrefix(href, "https://") ||
			strings.HasPrefix(href, "//") ||
			strings.HasPrefix(href, "#") {
			return
		}
		s.SetAttr("href", np.UpdateExt(path.Base(href)))
	})
	// TODO rename duplication of name css style

	np.Body, err = doc.Selection.Html()
	if err != nil {
		panic(err)
	}

	// TODO remove outer html tag
	x := xhtml{}
	d := xml.NewDecoder(bytes.NewReader([]byte(np.Body)))
	d.Strict = false
	err = d.Decode(&x)
	if err != nil {
		panic(err)
	}
	np.Body = x.Body.Inner
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

func FindLastSubNav(np *NavPoint) *NavPoint {
	if np.SubNavPoints == nil{
		return np
	}
	p := np.SubNavPoints[len(np.SubNavPoints) - 1]
	if p.SubNavPoints != nil {
		return FindLastSubNav(p)
	}
	return p
}

func (np *NavPoint) RelativePath(npx *NavPoint) string {
	rel, _ := filepath.Rel(np.Dir, npx.Content.Src)
	//fmt.Printf("np.Dir: %s, npx.Content.Src: %s, rel: %s\n", np.Dir, npx.Content.Src, rel)
	return rel
}

func (np *NavPoint) BasePath(npx *NavPoint) string {
	return path.Base(npx.Content.Src)
}

func (np *NavPoint) UpdateExt(orig string) string {
	if strings.HasPrefix(path.Ext(orig), ".xhtml") {
		idx := strings.LastIndex(orig, ".")
		orig = strings.Replace(orig, ".xhtml", ".html", idx)
	}
	if strings.HasPrefix(path.Ext(orig), ".jpg") {
		idx := strings.LastIndex(orig, ".")
		orig = strings.Replace(orig, ".jpg", ".html", idx)
	}
	return orig
}

func (np *NavPoint) TrimHash(orig string) string {
	if strings.Contains(orig, "html#") {
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
