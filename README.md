# Epub2Website - An epub to website converter

Now, support convert epub to website like [Honkit](https://github.com/honkit/honkit) style.

# Guide

## Build binary

Run:

```shell
go build -o ./epub2websie cmd/epub2websie/main.go
```

## Generate a website

> Honkit&Gitbook need some static assets for web pages and plugins.
> By default, the `https://cdn.jim.plus/` is opened to public.

Run:

```shell
./epub2website -g https://cdn.jim.plus/ -e /path/to/book.epub -o /path/to/output/epub2website
```

## Integration with Calibre Web

1. Update epub2website path in `Basic Configuration` --> `External Binaries` --> `Path to Epub2Website Converter`
2. Update epub2website library in `Basic Configuration` --> `External Binaries` --> `Epub2Website Library Endpoint Settings`

### More about epub2website library in Calibre Web

You can use `https://cdn.jim.plus/` for default epub2website library.

All files the epub2website needed is in [src](./src).

#### Alternative epub2website library

You can also copy all assets in [src](./src) to `calibre-web/cps/static`. Then set `Basic Configuration` --> `External Binaries` --> `Epub2Website Library Endpoint Settings` to `/static/`.

> Confirm gitbook directory locates with `calibre-web/cps/static/gitbook`.

# Supported plugin

All plugin should be updated in [./template/navigation.html](./template/navigation.html) and [./template/page.html](./template/page.html)

* gitbook-plugin-tbfed-pagefooter
* gitbook-plugin-back-to-top-button
* gitbook-plugin-page-toc-button
* gitbook-plugin-highlight
* gitbook-plugin-search-plus
* gitbook-plugin-fontsettings
* gitbook-plugin-expandable-chapters
* gitbook-plugin-splitte