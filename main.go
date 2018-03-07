package main

import (
	"os"
	"fmt"
	"flag"
	"io/ioutil"

	"github.com/jim3ma/epub2website/epub"
	"github.com/mholt/archiver"
)

var (
	workdir    string
	output     string
	gitbookUrl string
	epubFile   string
)

func init() {
	flag.StringVar(&workdir, "w", "", "work directory")
	flag.StringVar(&output, "o", "output", "output directory, must be a not exist directory")
	flag.StringVar(&gitbookUrl, "g", "https://cdn.jim.plus/", "gitbook library endpoint, like https://cdn.jim.plus/")
	flag.StringVar(&epubFile, "e", "", "epub book path")
}

//go:generate statik -src=./template
func main() {
	flag.Parse()
	var err error
	if workdir == "" {
		workdir, err = ioutil.TempDir("/tmp", "epub2website-")
		defer os.RemoveAll(workdir)
		if err != nil {
			panic(err)
		}
	}
	if output == "" || epubFile == "" {
		flag.Usage()
		defer os.Exit(1)
		return
	}

	err = archiver.Zip.Open(epubFile, workdir)
	if err != nil {
		defer os.Exit(1)
		return
	}

	firstPage, err := epub.Convert(output, workdir, gitbookUrl)
	if err != nil {
		panic(err)
	}
	fmt.Println(firstPage)
}
