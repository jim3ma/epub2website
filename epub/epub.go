package epub

import (
	"os"
	"path"
	"encoding/xml"
	"io/ioutil"

	"github.com/otiai10/copy"
)

func Convert(outputDir, unzipDir, gitbookUrl string) (string, error ){
	metaFile, err := os.OpenFile(path.Join(unzipDir, "META-INF", "container.xml"), os.O_RDONLY, 0644)
	if err != nil {
		return "", err
	}
	metaData, _ := ioutil.ReadAll(metaFile)
	metaInfo := &MetaInfo{}
	err = xml.Unmarshal(metaData, metaInfo)
	if err != nil {
		return "", err
	}
	//fmt.Printf("%#v\n", metaInfo)

	opfFile, err := os.OpenFile(path.Join(unzipDir, metaInfo.RootFile.Path), os.O_RDONLY, 0644)
	if err != nil {
		return "", err
	}
	opfData, _ := ioutil.ReadAll(opfFile)
	opf := &OPF{}
	err = xml.Unmarshal(opfData, opf)
	opf.Dir = path.Dir(metaInfo.RootFile.Path)
	if err != nil {
		return "", err
	}
	//fmt.Printf("%#v\n", opf)

	err = copy.Copy(path.Join(unzipDir, path.Dir(metaInfo.RootFile.Path)), outputDir)
	if err != nil {
		return "", err
	}
	ncxPath := path.Join(unzipDir, path.Dir(metaInfo.RootFile.Path), "toc.ncx")
	ncx, err := NewNcx(ncxPath, outputDir, gitbookUrl, opf)
	if err != nil {
		return "", err
	}
	firstPage, err := ncx.Render()
	if err != nil {
		return "", err
	}
	ncx.BuildIndex()
	return firstPage.UpdateExt(firstPage.Src), nil
}
