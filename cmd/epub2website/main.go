package main

import (
	"flag"
	"fmt"
	"io/ioutil"
	"os"

	"github.com/mholt/archiver/v3"

	"github.com/jim3ma/epub2website/epub"
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
	flag.StringVar(&gitbookUrl, "g", "", "gitbook library endpoint, like https://cdn.jim.plus/")
	flag.StringVar(&epubFile, "e", "", "epub book path")
}

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
	zip := &archiver.Zip{
		OverwriteExisting: true,
	}
	err = zip.Unarchive(epubFile, workdir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "unarchive error: %s\n", err)
		os.Exit(1)
	}

	firstPage, err := epub.Convert(output, workdir, gitbookUrl)
	if err != nil {
		panic(err)
	}
	fmt.Println(firstPage)
}
