package scraper

// What the change-detection cache stores for one section, and the two
// functions that get it in and out.
//
// It holds the extractor's candidates rather than a digested form, so a cache
// hit can replay appendSectionFiles and appendSectionFolderTargets against
// exactly what a fresh crawl would have given them. Storing files alone would
// have been the obvious shape and is wrong: crawl.go feeds the same candidates
// to appendSectionFolderTargets, so a hit returning only files leaves that
// section's subfolders permanently unqueued and the course comes back short
// with no warning.
//
// Only rootText is lifted out, and that is the whole size story. It is the
// section root's entire textContent, identical for every candidate in the
// section, read at exactly one place (deriveSectionTitle). Stored per candidate
// it was 31 MB of the 52 MB file the 2026-07-21 attempt produced for 276
// sections.
//
// Every other key is kept, deliberately, rather than whitelisted down to the
// nine currently read. A whitelist would silently drop a key the moment someone
// adds a use for it, and the cached path would then quietly behave differently
// from the crawled one - which is exactly the class of divergence this cache
// must not introduce.
type cachedSection struct {
	RootText   string              `json:"root_text"`
	Candidates []map[string]string `json:"candidates"`
}

const rootTextKey = "rootText"

func packSection(candidates []map[string]string) cachedSection {
	packed := cachedSection{Candidates: make([]map[string]string, 0, len(candidates))}
	for _, c := range candidates {
		trimmed := make(map[string]string, len(c))
		for k, v := range c {
			if k == rootTextKey {
				// Last one wins, but they are all the same value; that is the
				// property that makes this interning rather than lossy.
				packed.RootText = v
				continue
			}
			trimmed[k] = v
		}
		packed.Candidates = append(packed.Candidates, trimmed)
	}
	return packed
}

func unpackSection(packed cachedSection) []map[string]string {
	out := make([]map[string]string, 0, len(packed.Candidates))
	for _, c := range packed.Candidates {
		restored := make(map[string]string, len(c)+1)
		for k, v := range c {
			restored[k] = v
		}
		restored[rootTextKey] = packed.RootText
		out = append(out, restored)
	}
	return out
}
