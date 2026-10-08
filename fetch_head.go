package git

import (
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"

	"github.com/go-git/go-git/v6/config"
	giturl "github.com/go-git/go-git/v6/internal/url"
	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/object"
	"github.com/go-git/go-git/v6/plumbing/storer"
	"github.com/go-git/go-git/v6/utils/ioutil"
)

// fetchHeadPath is the file, relative to the git directory, where fetch
// records what it fetched.
const fetchHeadPath = "FETCH_HEAD"

// fetchHeadEntry is one line of FETCH_HEAD.
type fetchHeadEntry struct {
	hash plumbing.Hash
	// name is the remote reference that was fetched, or the object name
	// given by an exact-SHA refspec.
	name     string
	forMerge bool
}

// fetchHeadEntries lists the fetched references in the order and with the
// merge markers git uses for FETCH_HEAD.
//
// References matched by refspecs the caller gave are all for merge. With the
// remote's configured refspecs, only the current branch's upstream
// (branch.<name>.merge) is, or, when the branch has none, the first reference
// matched by a first refspec that is not a wildcard. Tags fetched by AllTags,
// or auto-followed, are never for merge, nor is anything that does not peel
// to a commit. Destinations that repeat are listed once. This is get_ref_map
// and store_updated_refs in builtin/fetch.c:
//
// https://github.com/git/git/blob/1630431f326e15fcde608827b5ff38422528eb59/builtin/fetch.c#L503-L610
// https://github.com/git/git/blob/1630431f326e15fcde608827b5ff38422528eb59/builtin/fetch.c#L1260-L1310
func (r *Remote) fetchHeadEntries(
	specs []config.RefSpec,
	specToRefs [][]*plumbing.Reference,
	remoteRefs storer.ReferenceStorer,
	followedTags []*plumbing.Reference,
	explicitRefSpecs bool,
) ([]fetchHeadEntry, error) {
	var entries []fetchHeadEntry
	seen := make(map[plumbing.ReferenceName]bool)
	for i, refs := range specToRefs {
		// calculateRefs appends the AllTags refspec after the given ones.
		spec := config.RefSpec(refspecAllTags)
		if i < len(specs) {
			spec = specs[i]
		}

		// A wildcard lists its matches in the order the remote advertised
		// them, which is by name.
		if spec.IsWildcard() {
			refs = slices.SortedFunc(slices.Values(refs), compareRefNames)
		}

		for _, ref := range refs {
			name := ref.Name().String()
			switch {
			case spec.IsExactSHA1():
				name = spec.Src()
			case !spec.IsWildcard():
				// The ref was found through symbolic references, but git
				// names the one the refspec matched, such as HEAD.
				for _, rule := range plumbing.RefRevParseRules {
					n := fmt.Sprintf(rule, spec.Src())
					if _, err := remoteRefs.Reference(plumbing.ReferenceName(n)); err == nil {
						name = n
						break
					}
				}
			}

			// A destination that is a hash stores no reference, as in
			// updateLocalReferenceStorage, so like a refspec without a
			// destination it is never a duplicate.
			if dst := spec.Dst(ref.Name()); !plumbing.IsHash(dst.String()) {
				if seen[dst] {
					continue
				}
				seen[dst] = true
			}

			entries = append(entries, fetchHeadEntry{
				hash:     ref.Hash(),
				name:     name,
				forMerge: explicitRefSpecs && i < len(specs),
			})
		}
	}

	if !explicitRefSpecs {
		if err := r.markUpstreamForMerge(entries, specs, specToRefs); err != nil {
			return nil, err
		}
	}

	for _, tag := range slices.SortedFunc(slices.Values(followedTags), compareRefNames) {
		entries = append(entries, fetchHeadEntry{hash: tag.Hash(), name: tag.Name().String()})
	}

	for i := range entries {
		if entries[i].forMerge && !r.peelsToCommit(entries[i].hash) {
			entries[i].forMerge = false
		}
	}

	return entries, nil
}

func compareRefNames(a, b *plumbing.Reference) int {
	return strings.Compare(a.Name().String(), b.Name().String())
}

// markUpstreamForMerge marks the entry git would merge after a fetch with the
// remote's configured refspecs.
func (r *Remote) markUpstreamForMerge(entries []fetchHeadEntry, specs []config.RefSpec, specToRefs [][]*plumbing.Reference) error {
	var branch *config.Branch
	head, err := r.s.Reference(plumbing.HEAD)
	if err != nil && !errors.Is(err, plumbing.ErrReferenceNotFound) {
		return err
	}
	if head != nil && head.Type() == plumbing.SymbolicReference && head.Target().IsBranch() {
		cfg, err := r.s.Config()
		if err != nil {
			return err
		}
		branch = cfg.Branches[head.Target().Short()]
	}

	if branch == nil || branch.Merge == "" {
		if len(entries) > 0 && len(specs) > 0 && !specs[0].IsWildcard() && len(specToRefs[0]) > 0 {
			entries[0].forMerge = true
		}
		return nil
	}

	if branch.Remote != r.c.Name {
		return nil
	}

	for i := range entries {
		for _, rule := range plumbing.RefRevParseRules {
			if fmt.Sprintf(rule, branch.Merge) == entries[i].name {
				entries[i].forMerge = true
				return nil
			}
		}
	}

	return nil
}

// peelsToCommit reports whether h names a commit, directly or through
// annotated tags.
func (r *Remote) peelsToCommit(h plumbing.Hash) bool {
	obj, err := object.GetObject(r.s, h)
	for err == nil {
		switch o := obj.(type) {
		case *object.Commit:
			return true
		case *object.Tag:
			obj, err = o.Object()
		default:
			return false
		}
	}
	return false
}

// truncateFetchHead empties FETCH_HEAD, as git does at the start of a fetch
// that does not append to it.
func (r *Remote) truncateFetchHead() error {
	fss, ok := r.s.(storer.FilesystemStorer)
	if !ok {
		return nil
	}

	f, err := fss.Filesystem().Create(fetchHeadPath)
	if err != nil {
		return err
	}
	return f.Close()
}

// writeFetchHead appends entries to FETCH_HEAD, those for merge first so that
// FETCH_HEAD resolves to the first of them.
func (r *Remote) writeFetchHead(entries []fetchHeadEntry, remoteURL string) (err error) {
	fss, ok := r.s.(storer.FilesystemStorer)
	if !ok {
		return nil
	}

	url := fetchHeadURL(remoteURL)
	var b strings.Builder
	for _, forMerge := range []bool{true, false} {
		for _, e := range entries {
			if e.forMerge != forMerge {
				continue
			}

			marker := "not-for-merge"
			if e.forMerge {
				marker = ""
			}
			fmt.Fprintf(&b, "%s\t%s\t%s%s\n", e.hash, marker, fetchHeadNote(e.name), url)
		}
	}

	f, err := fss.Filesystem().OpenFile(fetchHeadPath, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0o666)
	if err != nil {
		return err
	}
	defer ioutil.CheckClose(f, &err)

	_, err = f.Write([]byte(b.String()))
	return err
}

// fetchHeadNote describes a fetched reference the way git does in FETCH_HEAD,
// ready to be followed by the remote URL: "branch 'main' of ", "tag 'v1' of ",
// "'refs/pull/1/head' of ", or nothing for HEAD. See store_updated_refs in
// builtin/fetch.c:
//
// https://github.com/git/git/blob/1630431f326e15fcde608827b5ff38422528eb59/builtin/fetch.c#L1324-L1343
func fetchHeadNote(name string) string {
	if name == "HEAD" {
		return ""
	}

	for _, k := range []struct{ prefix, kind string }{
		{"refs/heads/", "branch "},
		{"refs/tags/", "tag "},
		{"refs/remotes/", "remote-tracking branch "},
	} {
		if what, ok := strings.CutPrefix(name, k.prefix); ok {
			return k.kind + "'" + what + "' of "
		}
	}

	return "'" + name + "' of "
}

// fetchHeadURL renders a remote URL the way git does in FETCH_HEAD: without
// credentials, trailing slashes, or a ".git" suffix, and with newlines
// escaped. See display_state_init and append_fetch_head in builtin/fetch.c:
//
// https://github.com/git/git/blob/1630431f326e15fcde608827b5ff38422528eb59/builtin/fetch.c#L729-L748
// https://github.com/git/git/blob/1630431f326e15fcde608827b5ff38422528eb59/builtin/fetch.c#L1164-L1196
func fetchHeadURL(rawURL string) string {
	url := strings.TrimRight(anonymizeURL(rawURL), "/")
	if len(url) > len(".git")+1 {
		url = strings.TrimSuffix(url, ".git")
	}
	return strings.ReplaceAll(url, "\n", `\n`)
}

// anonymizeURL removes the user information from a URL, as git does before
// showing it. This is transport_anonymize_url in transport.c:
//
// https://github.com/git/git/blob/1630431f326e15fcde608827b5ff38422528eb59/transport.c#L1744-L1784
func anonymizeURL(url string) string {
	at := strings.IndexByte(url, '@')
	if at < 0 || giturl.IsLocalEndpoint(url) {
		return url
	}
	anon := url[at+1:]

	scheme := strings.Index(url, "://")
	if scheme < 0 {
		// Only "user@host:path" has user information to remove.
		if !strings.Contains(anon, ":") {
			return url
		}
		return anon
	}

	for _, c := range url[:scheme] {
		isAlnum := (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9')
		if !isAlnum && c != '+' && c != '.' && c != '-' {
			return url
		}
	}

	// An @ past the first slash after the scheme is part of the path.
	authority := scheme + len("://")
	if slash := strings.IndexByte(url[authority:], '/'); slash >= 0 && authority+slash < at {
		return url
	}

	return url[:authority] + anon
}
