package handlers

import (
	"hash/fnv"
	"io"
	"io/fs"
	"strconv"
)

// assetVersion is a content hash over every embedded static file, computed once
// at startup and appended to CSS/JS URLs as ?v=…
//
// Assets are served under fixed names with a one-day max-age, so a browser that
// loaded the site earlier keeps its copies for up to 24 hours — while the HTML,
// which is not cached, updates immediately on deploy. That pairing of new markup
// with old CSS and JS is not a cosmetic risk: when the account-menu rework
// replaced #script-toggle with segmented controls, cached opencc-toggle.js hit
// its `if (!btn) return` guard and the 繁簡 control silently did nothing for
// every returning reader.
//
// Hashing the whole tree rather than each file keeps this to one value with no
// build step: any asset change busts every asset URL, which costs one extra
// round of downloads per deploy and removes the entire class of bug.
var assetVersion = computeAssetVersion()

func computeAssetVersion() string {
	h := fnv.New64a()
	err := fs.WalkDir(assetsFS, "static", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		f, err := assetsFS.Open(path)
		if err != nil {
			return err
		}
		defer f.Close()
		if _, err := io.WriteString(h, path); err != nil {
			return err
		}
		_, err = io.Copy(h, f)
		return err
	})
	if err != nil {
		// Embedded FS walks do not fail in practice; a fixed value still yields
		// working URLs, just without cache busting for that build.
		return "0"
	}
	return strconv.FormatUint(h.Sum64(), 36)
}

// assetURL returns the URL for an embedded static file, carrying the build's
// content hash so caches key on content instead of on filename alone.
func assetURL(name string) string {
	return "/assets/" + name + "?v=" + assetVersion
}
