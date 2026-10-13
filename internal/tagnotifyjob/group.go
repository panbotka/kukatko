package tagnotifyjob

import (
	"cmp"
	"slices"
)

// MaxAlbumGroups is the most albums one window announces separately. A window
// is one notification per album the newly tagged photos are in, so a person
// tagged across many albums in one afternoon would otherwise get as many
// pushes and inbox entries as there are albums — the very spam the window
// exists to prevent. Beyond this many, the remaining albums' photos join the
// "other photos" notification instead of getting one each; the mail is one per
// window anyway. A constant rather than a configuration key: it is a sanity cap
// on a fan-out, not a tuning knob anybody should need to turn.
const MaxAlbumGroups = 5

// consumedPhoto is one photo of a closing window that may still be named, with
// the project it belongs to — the project a tag notice is recorded in is always
// its photo's.
type consumedPhoto struct {
	UID        string
	ProjectUID string
}

// projectWindow is the part of a closing window inside one project: the
// project and its photos, newest first. It is announced on its own — its own
// notifications in that project's inbox, its own e-mail — so nothing about one
// project is ever said inside another.
type projectWindow struct {
	ProjectUID string
	Photos     []string
}

// groupByProject splits a closing window's photos — newest first — into one
// window per project, each keeping the photos' order. The projects come in the
// order of their newest photo, so the most recent tagging is announced first.
// A photo listed twice counts once. The result is deterministic for the same
// input. It is a pure function.
func groupByProject(photos []consumedPhoto) []projectWindow {
	var out []projectWindow
	index := make(map[string]int)
	seen := make(map[string]struct{}, len(photos))
	for _, p := range photos {
		if _, dup := seen[p.UID]; dup {
			continue
		}
		seen[p.UID] = struct{}{}
		i, ok := index[p.ProjectUID]
		if !ok {
			i = len(out)
			index[p.ProjectUID] = i
			out = append(out, projectWindow{ProjectUID: p.ProjectUID})
		}
		out[i].Photos = append(out[i].Photos, p.UID)
	}
	return out
}

// albumRef is one album a tagged photo is in, as read from album_photos.
type albumRef struct {
	UID     string
	Title   string
	Private bool
}

// taggedPhoto is one photo that survived the window's re-check, with the albums
// it is in.
type taggedPhoto struct {
	UID    string
	Albums []albumRef
}

// group is one notification's worth of a window: the newly tagged photos of
// one album, or — with a nil Album — the photos no album notification names.
type group struct {
	// Album is the album the group is about; nil for the "other photos" group.
	Album *albumRef
	// Photos are the group's photos, newest first.
	Photos []string
}

// albumGroup is a group under construction: the album, the position of its
// newest photo in the window, and the photos collected so far.
type albumGroup struct {
	ref    albumRef
	newest int
	photos []string
}

// groupByAlbum splits the window's photos — newest first — into the groups it
// is announced as: one per album the photos are in, ordered by the newest
// tagged photo in each (ties by title, then uid), followed by one group of
// "other photos" when any are left. A photo in several albums counts in each
// of them. A private album counts only when seesPrivate, the recipient's right
// to see it; otherwise its photos are "other". Only the first maxAlbums albums
// are kept: the rest collapse into "other", so the fan-out is bounded. "Other"
// holds every photo no kept album group names, in the window's order — a photo
// already announced under an album is not announced again as other. The
// result is deterministic for the same input. It is a pure function.
func groupByAlbum(photos []taggedPhoto, seesPrivate bool, maxAlbums int) []group {
	albums := albumGroups(photos, seesPrivate)
	kept := albums[:min(max(maxAlbums, 0), len(albums))]

	out := make([]group, 0, len(kept)+1)
	named := make(map[string]struct{})
	for _, g := range kept {
		out = append(out, group{Album: &g.ref, Photos: g.photos})
		for _, uid := range g.photos {
			named[uid] = struct{}{}
		}
	}
	var other []string
	for _, p := range photos {
		if _, ok := named[p.UID]; !ok {
			other = append(other, p.UID)
		}
	}
	if len(other) > 0 {
		out = append(out, group{Photos: other})
	}
	return out
}

// albumGroups collects every album the photos are in — a private one only when
// seesPrivate — with its photos in the window's order, sorted by the newest
// photo in each (ties by title, then uid). The same album twice on one photo
// counts once.
func albumGroups(photos []taggedPhoto, seesPrivate bool) []*albumGroup {
	byUID := make(map[string]*albumGroup)
	var albums []*albumGroup
	for i, p := range photos {
		seen := make(map[string]struct{}, len(p.Albums))
		for _, a := range p.Albums {
			if _, dup := seen[a.UID]; dup || (a.Private && !seesPrivate) {
				continue
			}
			seen[a.UID] = struct{}{}
			g := byUID[a.UID]
			if g == nil {
				g = &albumGroup{ref: a, newest: i}
				byUID[a.UID] = g
				albums = append(albums, g)
			}
			g.photos = append(g.photos, p.UID)
		}
	}
	slices.SortStableFunc(albums, func(x, y *albumGroup) int {
		return cmp.Or(cmp.Compare(x.newest, y.newest),
			cmp.Compare(x.ref.Title, y.ref.Title), cmp.Compare(x.ref.UID, y.ref.UID))
	})
	return albums
}
